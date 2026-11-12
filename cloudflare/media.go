package cloudflare

import (
	"bytes"
	"encoding/base64"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/media"
)

// maxRequestBytes is Clef's limit on a request body.
const maxRequestBytes = 13 << 20

// limits are Clef's documented image limits.
var limits = media.Limits{
	Provider:      "cloudflare",
	Takes:         "PNG, JPEG, and WebP images, as bytes",
	Types:         []string{"image/png", "image/jpeg", "image/webp"},
	MaxFiles:      4,
	MaxFileBytes:  4 << 20,
	MaxPixels:     16_000_000,
	MaxTotalBytes: 8 << 20,
}

func files(req *decide.Request) []media.File {
	out := make([]media.File, len(req.Media))
	for i, m := range req.Media {
		out[i] = media.File{Index: i, MIME: media.Normalize(m.MIMEType), Data: m.Data, URL: m.URL}
	}
	return out
}

// CheckMedia reports whether Clef takes req's files: at most four PNG,
// JPEG, or WebP images as bytes, 4 MiB and 16 megapixels each, 8 MiB in
// all, in a request of at most 13 MiB. A file it cannot take returns an
// error wrapping errors.ErrUnsupported. It reads image headers only.
func (t *Transport) CheckMedia(req *decide.Request) error {
	if len(req.Media) == 0 {
		return nil
	}
	return limits.Check(files(req))
}

// encodeBody writes the Workers AI body: decide's native JSON, then the
// images. Each image's base64 is written once, straight into the body.
func encodeBody(req *decide.Request) ([]byte, error) {
	native, err := req.MarshalJSON() // writes no media
	if err != nil {
		return nil, err
	}
	fs := files(req)
	size := len(native) + len(`,"images":[]`)
	for _, f := range fs {
		size += len(`{"content_type":"","base64":""},`) + len(f.MIME) + base64.StdEncoding.EncodedLen(len(f.Data))
	}
	if size > maxRequestBytes {
		return nil, limits.BodyTooLarge(int64(size), maxRequestBytes)
	}
	var buf bytes.Buffer
	buf.Grow(size)
	buf.Write(native[:len(native)-1]) // without the closing brace
	buf.WriteString(`,"images":[`)
	for i, f := range fs {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"content_type":"`)
		buf.WriteString(f.MIME) // one of limits.Types: no escaping needed
		buf.WriteString(`","base64":"`)
		media.AppendBase64(&buf, f.Data)
		buf.WriteString(`"}`)
	}
	buf.WriteString("]}")
	return buf.Bytes(), nil
}
