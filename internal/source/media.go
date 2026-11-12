package source

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // image.DecodeConfig reads JPEG headers
	_ "image/png"  // and PNG headers
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/template"
)

// Skipped counts the files a walk skipped, by reason. Files named
// directly are errors instead, and are not counted.
type Skipped struct {
	NotText      int // binary files, for a text template
	NotTextMedia int // of NotText, the images, PDFs, audio, and video
	Unsupported  int // files of a format the media template does not read
	TooLarge     int // media files over Options.MaxMediaBytes
	Pixels       int // images over Options.MaxPixels
	Invalid      int // media files whose contents fail a check: empty, corrupt, or another format
	Unreadable   int // files that could not be opened or read
}

// MediaError reports a media file, or media on stdin, given to a template
// that reads text.
type MediaError struct {
	Label string         // the file's label, or "stdin"
	Kind  template.Input // image, audio, video, or pdf
}

func (e *MediaError) Error() string {
	return fmt.Sprintf("%s is %s, and this template reads text", e.Label, MediaNoun(e.Kind))
}

// TooLargeError reports a named media file larger than
// Options.MaxMediaBytes.
type TooLargeError struct {
	Label string
	Input template.Input
	Size  int64
	Limit int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("%s is %s; %s files must be at most %s", e.Label, HumanBytes(e.Size), MediaName(e.Input), HumanBytes(e.Limit))
}

// InvalidMediaError reports a named media file whose contents fail a
// check: it is empty, corrupt, or in another format.
type InvalidMediaError struct {
	Label  string
	Input  template.Input
	Reason string // such as "empty" or "not a readable image"
}

func (e *InvalidMediaError) Error() string { return e.Label + " is " + e.Reason }

// PixelsError reports a named image with more pixels than
// Options.MaxPixels.
type PixelsError struct {
	Label         string
	Width, Height int
	Limit         int64  // the most pixels
	Provider      string // the provider that sets the limit, such as "Cloudflare"
}

func (e *PixelsError) Error() string {
	return fmt.Sprintf("%s is %d×%d pixels, and %s takes images up to %d megapixels",
		e.Label, e.Width, e.Height, e.Provider, e.Limit/1_000_000)
}

// UnsupportedError reports a named file whose extension is not one of the
// formats a media template reads, such as a GIF for an image template.
type UnsupportedError struct {
	Label string
	Input template.Input
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s is not in a format this template reads: %s", e.Label, MediaFormats(e.Input))
}

// NotTextError reports a named file or stdin that is not text, for a text
// template, when its format is not one a media template reads.
type NotTextError struct{ Label string }

func (e *NotTextError) Error() string { return e.Label + " is not text" }

// media reads one media file's header and makes an item that carries its
// path, not its contents, so a walk over large files holds little memory.
func (w *walker) media(path, label string, info fs.FileInfo, explicit bool) error {
	in := w.opts.Input
	// skip returns err for a named file. In a folder, it counts the file
	// under its reason, for one warning per reason after the walk.
	skip := func(counter *int, reason string, err error) error {
		if explicit {
			return err
		}
		*counter++
		w.skip(reason, label)
		return nil
	}
	invalid := func(reason string) error {
		return skip(&w.skipped.Invalid, reason, &InvalidMediaError{Label: label, Input: in, Reason: reason})
	}
	ctype, ok := mediaTypes[in][strings.ToLower(filepath.Ext(path))]
	if !ok {
		if explicit {
			return &UnsupportedError{Label: label, Input: in}
		}
		w.skipped.Unsupported++
		return nil
	}
	limit := w.opts.MaxMediaBytes
	if limit <= 0 {
		limit = MaxImageBytes
	}
	if info.Size() > limit {
		if explicit {
			return &TooLargeError{Label: label, Input: in, Size: info.Size(), Limit: limit}
		}
		on := "this provider"
		if w.opts.Provider != "" {
			on = w.opts.Provider
		}
		return skip(&w.skipped.TooLarge, fmt.Sprintf("over %s, the most %s takes for one %s file", HumanBytes(limit), on, MediaName(in)), nil)
	}
	if info.Size() == 0 {
		return invalid("empty")
	}
	f, err := os.Open(path)
	if err != nil {
		return w.unreadable(label, err, explicit)
	}
	defer f.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return w.unreadable(label, err, explicit)
	}
	head = head[:n]
	switch in {
	case template.Image:
		// Images are sniffed, since the providers check the header too.
		ctype = http.DetectContentType(head)
		switch ctype {
		case "image/png", "image/jpeg":
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return w.unreadable(label, err, explicit)
			}
			cfg, _, err := image.DecodeConfig(bufio.NewReader(f))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
				return invalid("not a readable image")
			}
			if most := w.opts.MaxPixels; most > 0 && int64(cfg.Width)*int64(cfg.Height) > most {
				return skip(&w.skipped.Pixels,
					fmt.Sprintf("over %d megapixels, the most %s takes for one image", most/1_000_000, w.opts.Provider),
					&PixelsError{Label: label, Width: cfg.Width, Height: cfg.Height, Limit: most, Provider: w.opts.Provider})
			}
		case "image/webp":
		default:
			return invalid("not a " + MediaFormats(in) + " image")
		}
	case template.PDF:
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return invalid("not a readable PDF")
		}
	}
	state, _ := json.Marshal(map[string]any{"path": label, "content_type": ctype})
	return w.emit(Item{Label: label, Unit: string(in), State: state,
		Image: &ImageData{ContentType: ctype, Path: path}})
}

// unreadable skips a file in a folder that could not be read, with a
// warning. A file named directly is an error.
func (w *walker) unreadable(label string, err error, explicit bool) error {
	if explicit {
		return err
	}
	w.skipped.Unreadable++
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err // the label already names the file
	}
	w.skip(fmt.Sprintf("could not read it: %v", err), label)
	return nil
}

// skipGroup is the files a walk skipped for one reason.
type skipGroup struct {
	reason string
	names  []string
}

// skip records a file the walk skipped, under its reason.
func (w *walker) skip(reason, label string) {
	for i := range w.skips {
		if w.skips[i].reason == reason {
			w.skips[i].names = append(w.skips[i].names, label)
			return
		}
	}
	w.skips = append(w.skips, skipGroup{reason: reason, names: []string{label}})
}

// notText handles a file that is not text, for a text template. A file in a
// folder is counted and skipped. A named file is an error that says which
// kind of media it is, when it is one.
func (w *walker) notText(path, label string, head []byte, explicit bool) error {
	kind := mediaKind(path, head)
	if !explicit {
		w.skipped.NotText++
		if kind != "" {
			w.skipped.NotTextMedia++
		}
		w.notTextNames = append(w.notTextNames, label)
		return nil
	}
	if kind != "" {
		return &MediaError{Label: label, Kind: kind}
	}
	return &NotTextError{Label: label}
}

// isBinary reports whether data, the start of a file or stream, is not
// text: it holds a NUL byte, starts like a known binary format, or more
// than 1 byte in 20 is a control character. Text that is not UTF-8, such
// as a CSV saved as Windows-1252, is text. Random bytes are about 10%
// control characters, and text has few or none.
func isBinary(data []byte) bool {
	head := data[:min(len(data), 8000)]
	if bytes.IndexByte(head, 0) >= 0 || sniffMedia(head) != "" || binaryFormat(head) {
		return true
	}
	controls := 0
	for _, b := range head {
		switch {
		case b == '\t', b == '\n', b == '\r', b == '\f', b == '\v', b == 0x1b: // 0x1b starts terminal colors
		case b < 0x20, b == 0x7f:
			controls++
		}
	}
	return controls*20 > len(head)
}

// binaryFormat reports whether data starts like an archive or a program.
func binaryFormat(data []byte) bool {
	for _, magic := range []string{
		"PK\x03\x04", "PK\x05\x06", // zip, and documents built on it, such as .docx
		"\x1f\x8b",                                                 // gzip
		"7z\xbc\xaf\x27\x1c",                                       // 7-Zip
		"Rar!\x1a\x07",                                             // RAR
		"\x7fELF",                                                  // Linux programs
		"\xca\xfe\xba\xbe", "\xcf\xfa\xed\xfe", "\xce\xfa\xed\xfe", // macOS programs
		"SQLite format 3\x00",              // SQLite
		"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1", // older Office documents
	} {
		if bytes.HasPrefix(data, []byte(magic)) {
			return true
		}
	}
	return false
}

// sniffMedia says which kind of media data starts like, or "" for none.
func sniffMedia(data []byte) template.Input {
	has := func(off int, magic string) bool {
		return len(data) >= off+len(magic) && string(data[off:off+len(magic)]) == magic
	}
	switch {
	case has(0, "%PDF-"):
		return template.PDF
	case has(0, "\x89PNG\r\n\x1a\n"), has(0, "\xff\xd8\xff"), has(0, "GIF87a"), has(0, "GIF89a"),
		has(0, "RIFF") && has(8, "WEBP"), has(0, "II*\x00"), has(0, "MM\x00*"):
		return template.Image
	case has(0, "RIFF") && has(8, "WAVE"), has(0, "FORM") && (has(8, "AIFF") || has(8, "AIFC")),
		has(0, "ID3") && len(data) > 3 && data[3] < 5, has(0, "OggS\x00"), has(0, "fLaC\x00"), has(0, "fLaC\x80"),
		len(data) >= 2 && data[0] == 0xff && bytes.IndexByte([]byte{0xfb, 0xfa, 0xf3, 0xf2, 0xe3, 0xe2, 0xf1, 0xf9}, data[1]) >= 0: // an MP3 or AAC frame
		return template.Audio
	case has(0, "RIFF") && has(8, "AVI "), has(0, "\x1a\x45\xdf\xa3"):
		return template.Video
	case has(4, "ftyp"):
		brand := ""
		if len(data) >= 12 {
			brand = string(data[8:12])
		}
		switch brand {
		case "heic", "heix", "hevc", "mif1", "msf1", "avif":
			return template.Image
		case "M4A ", "M4B ", "M4P ":
			return template.Audio
		}
		return template.Video
	}
	return ""
}

// otherImages are image formats no provider takes through decide, which a
// file's extension still names as images.
var otherImages = []string{".gif", ".heic", ".heif", ".avif", ".bmp", ".tif", ".tiff", ".ico"}

// mediaKind says which kind of media a file is, by its extension and then
// by its first bytes, or "" for none.
func mediaKind(path string, head []byte) template.Input {
	ext := strings.ToLower(filepath.Ext(path))
	for _, in := range []template.Input{template.Image, template.Audio, template.Video, template.PDF} {
		if _, ok := mediaTypes[in][ext]; ok {
			return in
		}
	}
	for _, e := range otherImages {
		if ext == e {
			return template.Image
		}
	}
	return sniffMedia(head)
}
