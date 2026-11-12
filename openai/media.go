package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/media"
)

// maxBody is the request limit in OpenAI's image guide.
const maxBody = 512_000_000

// limits are what the Decisions API takes: images only. The Decisions pages
// list no formats or limits, so the image guide's formats, without GIF, and
// its count apply.
var limits = media.Limits{
	Provider: "openai",
	Takes:    "PNG, JPEG, and WebP images, as bytes or by URL",
	Types:    []string{"image/png", "image/jpeg", "image/webp"},
	URLKinds: []string{media.Image},
	MaxFiles: 1500,
	Decimal:  true,
}

func files(req *decide.Request) []media.File {
	out := make([]media.File, len(req.Media))
	for i, m := range req.Media {
		out[i] = media.File{Index: i, MIME: media.Normalize(m.MIMEType), Data: m.Data, URL: m.URL}
	}
	return out
}

// CheckMedia reports whether the Decisions API takes req's files: PNG,
// JPEG, and WebP images as bytes or by URL, at most 1,500, in a request of
// at most 512 MB. A file it cannot take returns an error wrapping
// errors.ErrUnsupported.
func (t *Transport) CheckMedia(req *decide.Request) error {
	if len(req.Media) == 0 {
		return nil
	}
	if _, ok := req.Extra["images"]; ok {
		return invalid(`set images with decide.WithMedia or Extra["images"], not both`)
	}
	return limits.Check(files(req))
}

// encodeMedia writes body with its input as one user message: the state as
// input_text, unless it is empty, then one input_image part per file. Each
// file's base64 is written once, straight into the body. Extra members
// follow in key order, unless the body uses the key.
func encodeMedia(req *decide.Request, body decisionRequest, state string) ([]byte, error) {
	if err := limits.Check(files(req)); err != nil {
		return nil, err
	}
	model, _ := json.Marshal(body.Model)
	questions, err := json.Marshal(body.Questions)
	if err != nil {
		return nil, fmt.Errorf("%w: openai request: %w", decide.ErrInvalidRequest, err)
	}
	var text []byte
	if state != "" {
		text, _ = json.Marshal(state)
	}
	type member struct {
		key   string
		value []byte
	}
	var extra []member
	for _, k := range slices.Sorted(maps.Keys(req.Extra)) {
		if k == "model" || k == "input" || k == "questions" {
			continue
		}
		b, err := json.Marshal(req.Extra[k])
		if err != nil {
			return nil, fmt.Errorf("%w: openai request field %q: %w", decide.ErrInvalidRequest, k, err)
		}
		key, _ := json.Marshal(k)
		extra = append(extra, member{string(key), b})
	}
	urls := make([][]byte, len(req.Media))
	size := len(model) + len(questions) + len(text) + 128
	for i, m := range req.Media {
		if m.URL != "" {
			urls[i], _ = json.Marshal(m.URL)
			size += len(urls[i])
		} else {
			size += len(m.MIMEType) + base64.StdEncoding.EncodedLen(len(m.Data))
		}
		size += 64
	}
	for _, e := range extra {
		size += len(e.key) + len(e.value) + 2
	}
	if size > maxBody {
		return nil, limits.BodyTooLarge(int64(size), maxBody)
	}
	var buf bytes.Buffer
	buf.Grow(size)
	buf.WriteString(`{"model":`)
	buf.Write(model)
	buf.WriteString(`,"input":[{"role":"user","content":[`)
	if text != nil {
		buf.WriteString(`{"type":"input_text","text":`)
		buf.Write(text)
		buf.WriteString(`}`)
	}
	for i, m := range req.Media {
		if i > 0 || text != nil {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"type":"input_image","image_url":`)
		if m.URL != "" {
			buf.Write(urls[i])
		} else {
			buf.WriteString(`"data:`)
			buf.WriteString(media.Normalize(m.MIMEType)) // one of limits.Types: no escaping needed
			buf.WriteString(`;base64,`)
			media.AppendBase64(&buf, m.Data)
			buf.WriteByte('"')
		}
		buf.WriteByte('}')
	}
	buf.WriteString(`]}],"questions":`)
	buf.Write(questions)
	for _, e := range extra {
		buf.WriteByte(',')
		buf.WriteString(e.key)
		buf.WriteByte(':')
		buf.Write(e.value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
