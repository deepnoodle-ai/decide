package decide

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/deepnoodle-ai/decide/internal/media"
)

// MediaKind is the kind of file a request carries. "" means text.
type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaAudio MediaKind = "audio"
	MediaVideo MediaKind = "video"
	MediaPDF   MediaKind = "pdf"
)

// Media is a file a request carries with its state: an image, audio clip,
// video, or PDF. Set Data to send the file's bytes, or URL to have the
// provider fetch it. MIMEType is required in both cases.
//
// MIME types are matched without case and parameters, and common aliases
// are accepted, such as image/jpg, video/quicktime, and the names Go's
// mime.TypeByExtension and http.DetectContentType return. A PNG, JPEG,
// GIF, or WebP must have the bytes of that format. Other bytes of a format
// decide recognizes must be of MIMEType's kind: FLAC bytes declared as a
// PDF fail.
//
// Providers take different kinds and limits; see the backend package. A
// provider that cannot take a file returns an error wrapping
// errors.ErrUnsupported before any network call.
type Media struct {
	MIMEType string // such as "image/png" or "application/pdf"
	Data     []byte // the file's bytes; not copied, so do not change them during a call
	URL      string // an https URL, such as a Gemini Files API URI
}

// Kind returns the kind of m's MIME type, or "" for a type that is not an
// image, audio, video, or PDF type. A kind does not mean a provider takes
// the file: image/svg+xml is an image, and no provider takes it.
func (m Media) Kind() MediaKind {
	return MediaKind(media.Kind(media.Normalize(m.MIMEType)))
}

// String describes m without its bytes, such as "image/png, 48213 bytes".
// A URL is shown without its query or fragment, which can hold a token.
func (m Media) String() string {
	if m.URL != "" {
		u, err := url.Parse(m.URL)
		if err != nil {
			return m.MIMEType + ", an invalid URL"
		}
		u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
		return m.MIMEType + ", " + u.String()
	}
	return m.MIMEType + ", " + strconv.Itoa(len(m.Data)) + " bytes"
}

// WithMedia appends files to the request. Files are sent in order.
func WithMedia(files ...Media) RequestOption {
	return func(r *Request) { r.Media = append(r.Media, files...) }
}

// mediaAttemptTimeout is the default attempt timeout of a request that
// carries media. Uploading a large file takes longer than a text request.
const mediaAttemptTimeout = 5 * time.Minute

// validateMedia runs the checks that fail on every provider.
func validateMedia(files []Media) error {
	for i, m := range files {
		at := fmt.Sprintf("media[%d]", i)
		if m.MIMEType == "" {
			return invalidRequest(at + " has no MIMEType")
		}
		t := media.Normalize(m.MIMEType)
		if media.Kind(t) == "" {
			return invalidRequest(fmt.Sprintf("%s MIME type %q is not an image, audio, video, or PDF type", at, m.MIMEType))
		}
		switch {
		case m.Data != nil && m.URL != "":
			return invalidRequest(at + " sets both Data and URL; set one")
		case m.Data == nil && m.URL == "":
			return invalidRequest(at + " has no Data or URL")
		case m.Data != nil && len(m.Data) == 0:
			return invalidRequest(at + " Data is empty")
		}
		if m.URL != "" {
			u, err := url.Parse(m.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				return invalidRequest(at + " URL must be an absolute https URL")
			}
			continue
		}
		if looks, bad := media.Contradicts(t, m.Data); bad {
			if looks == "" {
				return invalidRequest(fmt.Sprintf("%s is declared %s, but its bytes are not that format", at, m.MIMEType))
			}
			return invalidRequest(fmt.Sprintf("%s is declared %s, but its bytes are %s", at, m.MIMEType, looks))
		}
		if media.HasHeader(t) {
			if _, _, err := media.Dimensions(t, m.Data); err != nil {
				return invalidRequest(fmt.Sprintf("%s has an %v", at, err))
			}
		}
	}
	return nil
}
