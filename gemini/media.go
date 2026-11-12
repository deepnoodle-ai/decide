package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/media"
)

// types are the MIME types the Decisions API takes, from its guide.
var types = []string{
	"image/png", "image/jpeg", "image/webp", "image/heic", "image/heif", "image/gif", "image/bmp", "image/tiff",
	"audio/wav", "audio/mp3", "audio/mpeg", "audio/aiff", "audio/aac", "audio/ogg", "audio/flac", "audio/m4a",
	"audio/opus", "audio/webm", "audio/l16", "audio/alaw", "audio/mulaw",
	"video/mp4", "video/mpeg", "video/mov", "video/avi", "video/webm", "video/wmv", "video/x-flv", "video/3gpp",
	"application/pdf",
}

// limits are the Decisions API's documented inline limits. They count
// base64 text.
var limits = media.Limits{
	Provider: "gemini",
	Takes:    "images, audio, video, and PDFs, as bytes or by HTTPS URL",
	Types:    types,
	URLKinds: []string{media.Image, media.Audio, media.Video, media.PDF},
	CheckURL: func(u *url.URL) string {
		if media.IsYouTube(u) {
			return "YouTube URLs"
		}
		return ""
	},
	MaxBase64:    100_000_000,
	MaxBase64PDF: 50_000_000,
	Decimal:      true,
}

// blockTypes maps a media kind to its input block type.
var blockTypes = map[string]string{media.Image: "image", media.Audio: "audio", media.Video: "video", media.PDF: "document"}

func files(req *decide.Request) []media.File {
	out := make([]media.File, len(req.Media))
	for i, m := range req.Media {
		out[i] = media.File{Index: i, MIME: media.Normalize(m.MIMEType), Data: m.Data, URL: m.URL}
	}
	return out
}

// CheckMedia reports whether the Decisions API takes req's files: images,
// audio, video, and PDFs of the types its guide lists, as bytes or by
// HTTPS URL other than YouTube, with at most 100 MB of inline base64, or
// 50 MB when a PDF is inline. A file it cannot take returns an error
// wrapping errors.ErrUnsupported.
func (t *Transport) CheckMedia(req *decide.Request) error {
	if len(req.Media) == 0 {
		return nil
	}
	return limits.Check(files(req))
}

// encodeMedia writes body with its input as blocks: the state as a text
// block, unless it is empty, then one block per file. Each file's base64 is
// written once, straight into the body. Extra members follow in key order,
// unless the body uses the key.
func encodeMedia(req *decide.Request, body decisionRequest, state string) ([]byte, error) {
	fs := files(req)
	if err := limits.Check(fs); err != nil {
		return nil, err
	}
	model, _ := json.Marshal(body.Model)
	questions, err := json.Marshal(body.Questions)
	if err != nil {
		return nil, fmt.Errorf("%w: gemini request: %w", decide.ErrInvalidRequest, err)
	}
	var text []byte
	if state != "" {
		text, _ = json.Marshal(state + "\n")
	}
	type member struct{ key, value []byte }
	var extra []member
	for _, k := range slices.Sorted(maps.Keys(req.Extra)) {
		if k == "model" || k == "input" || k == "questions" {
			continue
		}
		b, err := json.Marshal(req.Extra[k])
		if err != nil {
			return nil, fmt.Errorf("%w: gemini request field %q: %w", decide.ErrInvalidRequest, k, err)
		}
		key, _ := json.Marshal(k)
		extra = append(extra, member{key, b})
	}
	urls := make([][]byte, len(fs))
	size := len(model) + len(questions) + len(text) + 128
	for i, f := range fs {
		size += 64 + len(f.MIME)
		if f.URL != "" {
			urls[i], _ = json.Marshal(f.URL)
			size += len(urls[i])
		} else {
			size += base64.StdEncoding.EncodedLen(len(f.Data))
		}
	}
	for _, e := range extra {
		size += len(e.key) + len(e.value) + 2
	}
	var buf bytes.Buffer
	buf.Grow(size)
	buf.WriteString(`{"model":`)
	buf.Write(model)
	buf.WriteString(`,"input":[`)
	if text != nil {
		buf.WriteString(`{"type":"text","text":`)
		buf.Write(text)
		buf.WriteByte('}')
	}
	for i, f := range fs {
		if i > 0 || text != nil {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"type":"`)
		buf.WriteString(blockTypes[f.Kind()])
		buf.WriteString(`","mime_type":"`)
		buf.WriteString(f.MIME) // one of types: no escaping needed
		if f.URL != "" {
			buf.WriteString(`","uri":`)
			buf.Write(urls[i])
		} else {
			buf.WriteString(`","data":"`)
			media.AppendBase64(&buf, f.Data)
			buf.WriteByte('"')
		}
		buf.WriteByte('}')
	}
	buf.WriteString(`],"questions":`)
	buf.Write(questions)
	for _, e := range extra {
		buf.WriteByte(',')
		buf.Write(e.key)
		buf.WriteByte(':')
		buf.Write(e.value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
