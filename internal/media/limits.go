package media

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
)

// File is one file of a request, as a provider checks it.
type File struct {
	Index int    // position in Request.Media, for messages
	MIME  string // normalized
	Data  []byte
	URL   string
}

// Kind returns the file's kind.
func (f File) Kind() string { return Kind(f.MIME) }

// Limits are what one provider takes. A zero limit means none.
type Limits struct {
	Provider string   // such as "cloudflare", in messages
	Takes    string   // what it takes, such as "PNG, JPEG, and WebP images, as bytes"
	Types    []string // normalized MIME types it takes
	URLKinds []string // kinds it takes by URL; nil: no URLs
	// CheckURL returns why the provider rejects u, or "".
	CheckURL func(u *url.URL) string

	MaxFiles      int
	MaxFileBytes  int64 // decoded bytes of each inline file
	MaxPixels     int64 // pixels of each image whose header decide reads
	MaxTotalBytes int64 // decoded bytes of all inline files
	MaxBase64     int64 // base64 bytes of all inline files
	MaxBase64PDF  int64 // MaxBase64 when any inline file is a PDF
	Decimal       bool  // print sizes in MB, not MiB
}

func (l Limits) unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s %s", errors.ErrUnsupported, l.Provider, fmt.Sprintf(format, args...))
}

// Size prints n bytes as MiB, or as MB when decimal.
func Size(n int64, decimal bool) string {
	unit, name := float64(1<<20), "MiB"
	if decimal {
		unit, name = 1e6, "MB"
	}
	v := float64(n) / unit
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d %s", int64(v), name)
	}
	return fmt.Sprintf("%.1f %s", v, name)
}

// Check returns an error wrapping errors.ErrUnsupported for the first file
// the provider cannot take, then for the request's count and totals. Files
// have passed the root package's checks.
func (l Limits) Check(files []File) error {
	pdf := false
	var total, b64 int64
	for _, f := range files {
		kind := f.Kind()
		if !slices.ContainsFunc(l.Types, func(t string) bool { return Kind(t) == kind }) {
			return l.unsupported("does not take %s (media[%d], %s); it takes %s", Plural(kind), f.Index, f.MIME, l.Takes)
		}
		if f.URL != "" {
			if !slices.Contains(l.URLKinds, kind) {
				if len(l.URLKinds) == 0 {
					return l.unsupported("does not take media by URL (media[%d]); attach the file's bytes in Data", f.Index)
				}
				return l.unsupported("does not take %s by URL (media[%d], %s); it takes %s", Plural(kind), f.Index, f.MIME, l.Takes)
			}
			if l.CheckURL != nil {
				u, _ := url.Parse(f.URL) // the root package checked it
				if why := l.CheckURL(u); why != "" {
					return l.unsupported("does not take %s (media[%d], %s)", why, f.Index, f.MIME)
				}
			}
		}
		if !slices.Contains(l.Types, f.MIME) {
			return l.unsupported("does not take %s (media[%d]); it takes %s", f.MIME, f.Index, l.Takes)
		}
		if f.URL != "" {
			continue
		}
		n := int64(len(f.Data))
		if l.MaxFileBytes > 0 && n > l.MaxFileBytes {
			return l.unsupported("takes %s of at most %s (media[%d] is %s, %s)",
				Plural(kind), Size(l.MaxFileBytes, l.Decimal), f.Index, Size(n, l.Decimal), f.MIME)
		}
		if l.MaxPixels > 0 && HasHeader(f.MIME) {
			w, h, err := Dimensions(f.MIME, f.Data)
			if err == nil && int64(w)*int64(h) > l.MaxPixels {
				return l.unsupported("takes %s of at most %s (media[%d] is %dx%d, %s)",
					Plural(kind), megapixels(l.MaxPixels), f.Index, w, h, f.MIME)
			}
		}
		total += n
		b64 += int64((len(f.Data) + 2) / 3 * 4)
		pdf = pdf || kind == PDF
	}
	if l.MaxFiles > 0 && len(files) > l.MaxFiles {
		return l.unsupported("takes at most %d files (the request has %d)", l.MaxFiles, len(files))
	}
	if l.MaxTotalBytes > 0 && total > l.MaxTotalBytes {
		return l.unsupported("takes at most %s of files in all (the request has %s)",
			Size(l.MaxTotalBytes, l.Decimal), Size(total, l.Decimal))
	}
	if limit := l.MaxBase64; limit > 0 {
		when := ""
		if pdf && l.MaxBase64PDF > 0 {
			limit, when = l.MaxBase64PDF, " when a PDF is inline"
		}
		if b64 > limit {
			return l.unsupported("takes at most %s of inline base64%s (the request has %s)",
				Size(limit, l.Decimal), when, Size(b64, l.Decimal))
		}
	}
	return nil
}

// BodyTooLarge is the error for a request body over a provider's limit.
func (l Limits) BodyTooLarge(size, limit int64) error {
	return l.unsupported("takes requests of at most %s (this one is %s)", Size(limit, l.Decimal), Size(size, l.Decimal))
}

func megapixels(n int64) string {
	if n%1_000_000 == 0 {
		return fmt.Sprintf("%d megapixels", n/1_000_000)
	}
	return fmt.Sprintf("%d pixels", n)
}
