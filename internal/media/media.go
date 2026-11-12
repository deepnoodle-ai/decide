// Package media holds the MIME types, file sniffing, and provider limits
// for media, shared by the root package and the provider packages. It
// imports only the standard library, so the root package may import it.
package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registers GIF for image.DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"slices"
	"strings"
)

// Kinds of media. They match decide.MediaKind.
const (
	Image = "image"
	Audio = "audio"
	Video = "video"
	PDF   = "pdf"
)

// aliases maps other names of a type to the name the providers list. It
// covers what Go's mime.TypeByExtension returns on macOS and Linux, and
// what http.DetectContentType returns, for the formats providers take.
var aliases = map[string]string{
	"image/jpg":       "image/jpeg",
	"audio/x-wav":     "audio/wav",
	"audio/wave":      "audio/wav",
	"audio/vnd.wave":  "audio/wav",
	"audio/x-m4a":     "audio/m4a",
	"audio/mp4":       "audio/m4a",
	"audio/mp4a-latm": "audio/m4a",
	"audio/x-aac":     "audio/aac",
	"audio/x-flac":    "audio/flac",
	"audio/x-aiff":    "audio/aiff",
	"application/ogg": "audio/ogg",
	"video/quicktime": "video/mov",
	"video/x-m4v":     "video/mp4",
	"video/x-msvideo": "video/avi",
	"video/vnd.avi":   "video/avi",
	"video/x-ms-wmv":  "video/wmv",
}

// Normalize lowers t, drops its parameters, and resolves known aliases,
// such as "Image/JPG; q=1" to "image/jpeg".
func Normalize(t string) string {
	t, _, _ = strings.Cut(t, ";")
	t = strings.ToLower(strings.TrimSpace(t))
	if a, ok := aliases[t]; ok {
		return a
	}
	return t
}

// Kind returns the kind of a normalized MIME type, or "" for a type that
// is not an image, audio, video, or PDF.
func Kind(t string) string {
	major, minor, ok := strings.Cut(t, "/")
	if !ok || minor == "" {
		return ""
	}
	switch {
	case major == "image" || major == "audio" || major == "video":
		return major
	case t == "application/pdf":
		return PDF
	}
	return ""
}

// Plural names a kind in an error message: "images", "audio", "videos",
// "PDFs".
func Plural(kind string) string {
	switch kind {
	case Image:
		return "images"
	case Video:
		return "videos"
	case PDF:
		return "PDFs"
	}
	return kind
}

// signature is a format with a fixed header and the MIME types it can be.
type signature struct {
	name  string
	match func([]byte) bool
	types []string
}

func prefix(p string) func([]byte) bool {
	return func(b []byte) bool { return bytes.HasPrefix(b, []byte(p)) }
}

func riff(form string) func([]byte) bool {
	return func(b []byte) bool { return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == form }
}

var signatures = []signature{
	{"PNG", prefix("\x89PNG\r\n\x1a\n"), []string{"image/png", "image/apng"}},
	{"JPEG", prefix("\xff\xd8\xff"), []string{"image/jpeg"}},
	{"GIF", func(b []byte) bool {
		return bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a"))
	}, []string{"image/gif"}},
	{"WebP", riff("WEBP"), []string{"image/webp"}},
	{"PDF", prefix("%PDF-"), []string{"application/pdf"}},
	{"WAV", riff("WAVE"), []string{"audio/wav"}},
	{"MP3", func(b []byte) bool {
		return bytes.HasPrefix(b, []byte("ID3")) || len(b) >= 2 && b[0] == 0xff && (b[1] == 0xfb || b[1] == 0xf3 || b[1] == 0xf2)
	}, []string{"audio/mpeg", "audio/mp3"}},
	{"Ogg", prefix("OggS"), []string{"audio/ogg", "audio/opus", "video/ogg"}},
	{"FLAC", prefix("fLaC"), []string{"audio/flac"}},
	{"MP4", func(b []byte) bool { return len(b) >= 12 && string(b[4:8]) == "ftyp" }, []string{
		"video/mp4", "video/mov", "video/3gpp", "video/3gpp2", "audio/m4a", "audio/aac",
		"image/heic", "image/heif", "image/avif"}},
	{"WebM", prefix("\x1a\x45\xdf\xa3"), []string{"video/webm", "audio/webm", "video/x-matroska"}},
}

// mustSign lists the types whose files always start with their signature.
// Bytes without it are not that format.
var mustSign = map[string]string{
	"image/png":  "PNG",
	"image/jpeg": "JPEG",
	"image/gif":  "GIF",
	"image/webp": "WebP",
}

// Contradicts reports whether data cannot be a file of the normalized type
// t, and names what the bytes look like ("PNG"), or "" when decide does
// not recognize them. A PNG, JPEG, GIF, or WebP type needs bytes with its
// own signature. For any other type, recognized bytes contradict t only
// when none of their format's types has t's kind: FLAC bytes declared as
// an audio type that decide does not know are left to the provider's list
// of types.
func Contradicts(t string, data []byte) (looksLike string, bad bool) {
	want, signed := mustSign[t]
	kind := Kind(t)
	for _, s := range signatures {
		if !s.match(data) {
			continue
		}
		if signed {
			return s.name, s.name != want
		}
		return s.name, !slices.ContainsFunc(s.types, func(st string) bool { return st == t || Kind(st) == kind })
	}
	return "", signed
}

// Dimensions reads the width and height from the header of a PNG, JPEG,
// GIF, or WebP. It does not decode pixels.
func Dimensions(t string, data []byte) (w, h int, err error) {
	switch t {
	case "image/webp":
		return webpDimensions(data)
	case "image/png", "image/jpeg", "image/gif":
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || "image/"+format != t {
			return 0, 0, fmt.Errorf("invalid %s header", strings.ToUpper(strings.TrimPrefix(t, "image/")))
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return 0, 0, fmt.Errorf("invalid %s header", strings.ToUpper(strings.TrimPrefix(t, "image/")))
		}
		return cfg.Width, cfg.Height, nil
	}
	return 0, 0, errors.ErrUnsupported
}

// HasHeader reports whether Dimensions reads t.
func HasHeader(t string) bool {
	switch t {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// webpDimensions reads only the bounded container and image headers.
func webpDimensions(data []byte) (int, int, error) {
	bad := errors.New("invalid WebP header")
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, bad
	}
	for offset := 12; offset+8 <= len(data); {
		chunkSize := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		if uint64(chunkSize) > uint64(len(data)-offset-8) {
			return 0, 0, bad
		}
		n := int(chunkSize) // bounded by the input length before conversion
		chunk := data[offset+8 : offset+8+n]
		switch string(data[offset : offset+4]) {
		case "VP8X":
			if len(chunk) < 10 {
				return 0, 0, bad
			}
			read24 := func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }
			return 1 + read24(chunk[4:7]), 1 + read24(chunk[7:10]), nil
		case "VP8L":
			if len(chunk) < 5 || chunk[0] != 0x2f {
				return 0, 0, bad
			}
			bits := binary.LittleEndian.Uint32(chunk[1:5])
			return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
		case "VP8 ":
			if len(chunk) < 10 || !bytes.Equal(chunk[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, bad
			}
			w := int(binary.LittleEndian.Uint16(chunk[6:8]) & 0x3fff)
			h := int(binary.LittleEndian.Uint16(chunk[8:10]) & 0x3fff)
			if w == 0 || h == 0 {
				return 0, 0, bad
			}
			return w, h, nil
		}
		offset += 8 + n + n%2
	}
	return 0, 0, bad
}

// IsYouTube reports whether u is on a YouTube host.
func IsYouTube(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return h == "youtube.com" || strings.HasSuffix(h, ".youtube.com") || h == "youtu.be"
}

// AppendBase64 writes data to buf as standard base64 without growing buf
// more than once.
func AppendBase64(buf *bytes.Buffer, data []byte) {
	buf.Grow(base64.StdEncoding.EncodedLen(len(data)))
	buf.Write(base64.StdEncoding.AppendEncode(buf.AvailableBuffer(), data))
}
