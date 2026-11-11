package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/deepnoodle-ai/decide"
)

// Kind is the type of a media block.
type Kind string

const (
	KindImage    Kind = "image"
	KindAudio    Kind = "audio"
	KindVideo    Kind = "video"
	KindDocument Kind = "document" // a PDF
)

// MIME types the Decisions API accepts for each kind.
var mimeTypes = map[Kind][]string{
	KindImage: {"image/png", "image/jpeg", "image/webp", "image/heic", "image/heif",
		"image/gif", "image/bmp", "image/tiff"},
	KindAudio: {"audio/wav", "audio/mp3", "audio/mpeg", "audio/aiff", "audio/aac",
		"audio/ogg", "audio/flac", "audio/m4a", "audio/opus", "audio/webm", "audio/l16",
		"audio/alaw", "audio/mulaw"},
	KindVideo: {"video/mp4", "video/mpeg", "video/mov", "video/avi", "video/webm",
		"video/wmv", "video/x-flv", "video/3gpp"},
	KindDocument: {"application/pdf"},
}

// Limits on inline data, counted as base64 text, from the API's docs.
const (
	maxInlineBytes    = 100_000_000
	maxInlinePDFBytes = 50_000_000
)

// Media is one image, audio clip, video, or PDF to send with a request. Set
// Data to embed the file, or URI to point at a Files API URI or an HTTPS URL.
// Resolution is "low", "medium", "high", or "ultra_high" for images, and
// "low" through "high" for video; empty leaves the API's default.
type Media struct {
	Kind       Kind   `json:"type"`
	MIMEType   string `json:"mime_type"`
	Data       string `json:"data,omitempty"` // base64
	URI        string `json:"uri,omitempty"`
	Resolution string `json:"resolution,omitempty"`
}

// NewMedia embeds data as kind, with its MIME type, such as
// NewMedia(KindDocument, "application/pdf", pdf).
func NewMedia(kind Kind, mimeType string, data []byte) (Media, error) {
	m := Media{Kind: kind, MIMEType: mimeType, Data: base64.StdEncoding.EncodeToString(data)}
	if len(data) == 0 {
		return Media{}, invalid(string(kind) + " is empty")
	}
	return m, m.check()
}

// MediaURI refers to media by a Files API URI or an HTTPS URL. The API
// fetches it; YouTube URLs and gs:// URIs are not accepted.
func MediaURI(kind Kind, mimeType, uri string) (Media, error) {
	m := Media{Kind: kind, MIMEType: mimeType, URI: uri}
	return m, m.check()
}

// MediaKind returns the kind that takes mimeType, and false when the API
// takes no media of that type.
func MediaKind(mimeType string) (Kind, bool) {
	for kind, types := range mimeTypes {
		if slices.Contains(types, mimeType) {
			return kind, true
		}
	}
	return "", false
}

func (m Media) check() error {
	types, ok := mimeTypes[m.Kind]
	if !ok {
		return invalid(fmt.Sprintf("media type %q must be image, audio, video, or document", m.Kind))
	}
	if !slices.Contains(types, m.MIMEType) {
		return invalid(fmt.Sprintf("%s does not take MIME type %q", m.Kind, m.MIMEType))
	}
	if (m.Data == "") == (m.URI == "") {
		return invalid("media needs data or a URI, not both")
	}
	if m.Data != "" {
		// Stream the check, so a large file is not decoded into memory.
		if _, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(m.Data))); err != nil {
			return invalid(string(m.Kind) + " contains invalid base64 data")
		}
	}
	if m.URI != "" {
		u, err := url.Parse(m.URI)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return invalid("media URI must be an HTTPS URL or a Files API URI")
		}
		if strings.Contains(u.Host, "youtube.com") || u.Host == "youtu.be" {
			return invalid("the API does not take YouTube URLs")
		}
	}
	switch m.Resolution {
	case "":
	case "low", "medium", "high":
		if m.Kind != KindImage && m.Kind != KindVideo {
			return invalid("only images and video take a resolution")
		}
	case "ultra_high":
		if m.Kind != KindImage {
			return invalid("only images take ultra_high resolution")
		}
	default:
		return invalid(fmt.Sprintf("resolution %q must be low, medium, high, or ultra_high", m.Resolution))
	}
	return nil
}

// SetMedia replaces req.Extra["media"] with a copy of media after
// validating them. It leaves req unchanged on failure. The transport sends
// the state as a text block, then each media block in order. Do not call it
// while req is in use by another goroutine.
func SetMedia(req *decide.Request, media ...Media) error {
	if req == nil {
		return invalid("request is nil")
	}
	copyMedia, err := mediaBlocks(map[string]any{"media": media})
	if err != nil {
		return err
	}
	if req.Extra == nil {
		req.Extra = make(map[string]any)
	}
	req.Extra["media"] = copyMedia
	return nil
}

// mediaBlocks reads Extra["media"], and Extra["images"] as
// cloudflare.SetImages writes it, and checks them against the API's limits.
func mediaBlocks(extra map[string]any) ([]Media, error) {
	var out []Media
	switch v := extra["media"].(type) {
	case nil:
	case []Media: // as SetMedia stores it: no JSON round trip for large data
		out = append(out, v...)
	default:
		raw, err := json.Marshal(v)
		if err != nil || json.Unmarshal(raw, &out) != nil {
			return nil, invalid("media must be a list of gemini.Media")
		}
	}
	if v, ok := extra["images"]; ok {
		var images []struct {
			ContentType string `json:"content_type"`
			Base64      string `json:"base64"`
		}
		raw, err := json.Marshal(v)
		if err != nil || json.Unmarshal(raw, &images) != nil {
			return nil, invalid("images must be content_type/base64 objects")
		}
		for _, im := range images {
			out = append(out, Media{Kind: KindImage, MIMEType: im.ContentType, Data: im.Base64})
		}
	}
	inline, limit := 0, maxInlineBytes
	for i, m := range out {
		if err := m.check(); err != nil {
			return nil, err
		}
		// Re-encode so line breaks that decoding skips never reach the API.
		if strings.ContainsAny(m.Data, "\r\n") {
			data, _ := base64.StdEncoding.DecodeString(m.Data)
			out[i].Data = base64.StdEncoding.EncodeToString(data)
		}
		inline += len(out[i].Data)
		if m.Kind == KindDocument {
			limit = maxInlinePDFBytes
		}
	}
	if inline > limit {
		return nil, invalid(fmt.Sprintf("inline media is %d bytes of base64; the limit is %d", inline, limit))
	}
	return out, nil
}
