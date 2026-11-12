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
	"slices"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/template"
)

// MediaLimits are a provider's limits for one kind of media file.
type MediaLimits struct {
	MaxBytes  int64 // the largest file; zero means MaxImageBytes
	MaxPixels int64 // the most pixels in an image; zero means no limit
}

// limits returns the provider's limits for kind, and whether it reads
// that kind at all.
func (o Options) limits(kind template.Input) (MediaLimits, bool) {
	if o.Media == nil {
		return MediaLimits{}, true
	}
	l, ok := o.Media[kind]
	return l, ok
}

// Skipped counts the files a walk skipped, by reason. Files named
// directly are errors instead, and are not counted.
type Skipped struct {
	NotText      int // binary files, for a text template
	NotTextMedia int // of NotText, the images, PDFs, audio, and video
	Unsupported  int // files of a format the media template does not read
	TooLarge     int // media files over their kind's MediaLimits.MaxBytes
	Pixels       int // images over MediaLimits.MaxPixels
	Invalid      int // media files whose contents fail a check: empty, corrupt, or another format
	Unreadable   int // files that could not be opened or read
	NotRead      int // media files of a kind the provider does not read

	// Media lists the media files of a format decide sends that a text
	// template's folder walk skipped, so a hint can show how to name them.
	Media []SkippedMedia
}

// SkippedMedia is a media file a folder walk skipped for a text template.
type SkippedMedia struct {
	Label string
	Kind  template.Input
}

// MediaError reports media on stdin, which decide reads as text only.
type MediaError struct {
	Label string         // "stdin"
	Kind  template.Input // image, audio, video, or pdf
}

func (e *MediaError) Error() string {
	return fmt.Sprintf("%s is %s, and this template reads text", e.Label, MediaNoun(e.Kind))
}

// ProviderError reports a named media file of a kind the provider does
// not read.
type ProviderError struct {
	Label    string
	Kind     template.Input
	Provider string // such as "OpenAI"
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s is %s, and %s does not read it", e.Label, MediaNoun(e.Kind), e.Provider)
}

// FormatError reports a named file of a media kind that decide does not
// send: a format it does not read, such as a GIF, or a media file whose
// name does not end in an extension decide reads.
type FormatError struct {
	Label     string
	Kind      template.Input
	ByContent bool // the name does not say it is media; its first bytes do
}

func (e *FormatError) Error() string {
	if e.ByContent {
		return fmt.Sprintf("%s is %s, and its name has no extension decide reads, such as %s",
			e.Label, MediaNoun(e.Kind), MediaExt(e.Kind))
	}
	return fmt.Sprintf("%s is %s, and decide sends %s only as %s",
		e.Label, MediaNoun(e.Kind), MediaPlural(e.Kind), MediaFormats(e.Kind))
}

// NamedErrors holds the files the user named that cannot be sent, in the
// order named. Walk returns it when more than one file fails.
type NamedErrors struct{ Errs []error }

func (e *NamedErrors) Error() string {
	msgs := make([]string, len(e.Errs))
	for i, err := range e.Errs {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("%d files can't be sent: %s", len(e.Errs), strings.Join(msgs, "; "))
}

// TooLargeError reports a named media file larger than its kind's
// MediaLimits.MaxBytes.
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
// MediaLimits.MaxPixels.
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

// UnsupportedError reports a named file that is not media, given to a
// media template.
type UnsupportedError struct {
	Label string
	Input template.Input
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s is not an image, audio, video, or PDF file, and this template reads %s", e.Label, MediaPlural(e.Input))
}

// NotTextError reports a named file or stdin that is not text, for a text
// template, when it is not media either.
type NotTextError struct{ Label string }

func (e *NotTextError) Error() string { return e.Label + " is not text" }

// checkNamed checks every file the user named that decide would send as
// media, before the walk reads anything, so that one error can name every
// file that cannot be sent. Folders, stdin, and paths that do not exist
// are left to the walk.
func (w *walker) checkNamed(paths []string) error {
	var errs []error
	for _, p := range paths {
		if p == "-" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := w.checkNamedFile(p, w.names[p], info); err != nil {
			errs = append(errs, err)
		}
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	return &NamedErrors{Errs: errs}
}

// checkNamedFile checks one named file: its format, that the provider
// reads its kind, and the provider's limits. A text file passes for a
// text template, and is left to the walk.
func (w *walker) checkNamedFile(path, label string, info fs.FileInfo) error {
	kind, sent := extKind(path)
	switch {
	case sent:
		_, err := w.checkMedia(path, label, info, kind)
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return nil // the walk reports a file it cannot read
		}
		return err
	case kind != "":
		return &FormatError{Label: label, Kind: kind}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil // the walk reports it
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	f.Close()
	if isBinary(head[:n]) {
		if k := sniffMedia(head[:n]); k != "" {
			return &FormatError{Label: label, Kind: k, ByContent: true}
		}
	}
	if w.opts.Input.Media() {
		return &UnsupportedError{Label: label, Input: w.opts.Input}
	}
	return nil
}

// errNotFormat reports a file in a folder whose extension is not one of
// the media template's formats.
var errNotFormat = errors.New("not in the template's formats")

// checkMedia runs the checks on one media file of a kind, in order: that
// the provider reads the kind, the provider's size limit, that the file
// is not empty, that its header is of its format, and the provider's
// pixel limit. It returns the file's MIME type.
func (w *walker) checkMedia(path, label string, info fs.FileInfo, kind template.Input) (string, error) {
	ctype, ok := mediaTypes[kind][strings.ToLower(filepath.Ext(path))]
	if !ok {
		return "", errNotFormat
	}
	l, ok := w.opts.limits(kind)
	if !ok {
		return "", &ProviderError{Label: label, Kind: kind, Provider: w.opts.Provider}
	}
	limit := l.MaxBytes
	if limit <= 0 {
		limit = MaxImageBytes
	}
	if info.Size() > limit {
		return "", &TooLargeError{Label: label, Input: kind, Size: info.Size(), Limit: limit}
	}
	if info.Size() == 0 {
		return "", &InvalidMediaError{Label: label, Input: kind, Reason: "empty"}
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	head = head[:n]
	switch kind {
	case template.Image:
		// Images are sniffed, since the providers check the header too.
		ctype = http.DetectContentType(head)
		switch ctype {
		case "image/png", "image/jpeg":
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return "", err
			}
			cfg, _, err := image.DecodeConfig(bufio.NewReader(f))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
				return "", &InvalidMediaError{Label: label, Input: kind, Reason: "not a readable image"}
			}
			if most := l.MaxPixels; most > 0 && int64(cfg.Width)*int64(cfg.Height) > most {
				return "", &PixelsError{Label: label, Width: cfg.Width, Height: cfg.Height, Limit: most, Provider: w.opts.Provider}
			}
		case "image/webp":
		default:
			return "", &InvalidMediaError{Label: label, Input: kind, Reason: "not a " + MediaFormats(kind) + " image"}
		}
	case template.PDF:
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return "", &InvalidMediaError{Label: label, Input: kind, Reason: "not a readable PDF"}
		}
	}
	return ctype, nil
}

// media makes an item of one media file that carries its path, not its
// contents, so a walk over large files holds little memory. A file the
// user named that fails a check is an error. In a folder, the file is
// counted and skipped, with one warning per reason after the walk.
func (w *walker) media(path, label string, info fs.FileInfo, kind template.Input, explicit bool) error {
	ctype, err := w.checkMedia(path, label, info, kind)
	if err != nil {
		if explicit && !errors.Is(err, errNotFormat) {
			return err
		}
		var (
			provider *ProviderError
			large    *TooLargeError
			pixels   *PixelsError
			invalid  *InvalidMediaError
		)
		switch {
		case errors.Is(err, errNotFormat):
			if explicit {
				return &UnsupportedError{Label: label, Input: kind}
			}
			w.skipped.Unsupported++
		case errors.As(err, &provider):
			w.skipped.NotRead++
			w.skip(fmt.Sprintf("%s does not read %s", w.opts.Provider, MediaPlural(kind)), label)
		case errors.As(err, &large):
			on := "this provider"
			if w.opts.Provider != "" {
				on = w.opts.Provider
			}
			w.skipped.TooLarge++
			w.skip(fmt.Sprintf("over %s, the most %s takes for one %s file", HumanBytes(large.Limit), on, MediaName(kind)), label)
		case errors.As(err, &pixels):
			w.skipped.Pixels++
			w.skip(fmt.Sprintf("over %d megapixels, the most %s takes for one image", pixels.Limit/1_000_000, w.opts.Provider), label)
		case errors.As(err, &invalid):
			w.skipped.Invalid++
			w.skip(invalid.Reason, label)
		default:
			return w.unreadable(label, err, false)
		}
		return nil
	}
	state, _ := json.Marshal(map[string]any{"path": label, "content_type": ctype})
	return w.emit(Item{Label: label, Unit: string(kind), State: state,
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

// notText handles a file that is not text, for a text template. A file in
// a folder is counted and skipped. A named file is an error; when its
// first bytes are media, its name does not say so, since a named file
// whose extension is media is read as media.
func (w *walker) notText(path, label string, head []byte, explicit bool) error {
	kind := mediaKind(path, head)
	if !explicit {
		w.skipped.NotText++
		if kind != "" {
			w.skipped.NotTextMedia++
			if _, sent := extKind(path); sent {
				w.skipped.Media = append(w.skipped.Media, SkippedMedia{Label: label, Kind: kind})
			}
		}
		w.notTextNames = append(w.notTextNames, label)
		return nil
	}
	if kind != "" {
		return &FormatError{Label: label, Kind: kind, ByContent: true}
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

// MediaExt is the usual extension of a media kind, such as ".png", for
// messages.
func MediaExt(kind template.Input) string {
	switch kind {
	case template.Audio:
		return ".mp3"
	case template.Video:
		return ".mp4"
	case template.PDF:
		return ".pdf"
	}
	return ".png"
}

// otherImages are image formats no provider takes through decide, which a
// file's extension still names as images.
var otherImages = []string{".gif", ".heic", ".heif", ".avif", ".bmp", ".tif", ".tiff", ".ico"}

// extKind says which kind of media a file's extension names, and whether
// decide sends files with that extension. A GIF is an image that decide
// does not send.
func extKind(path string) (kind template.Input, sent bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, in := range []template.Input{template.Image, template.Audio, template.Video, template.PDF} {
		if _, ok := mediaTypes[in][ext]; ok {
			return in, true
		}
	}
	if slices.Contains(otherImages, ext) {
		return template.Image, false
	}
	return "", false
}

// mediaKind says which kind of media a file is, by its extension and then
// by its first bytes, or "" for none.
func mediaKind(path string, head []byte) template.Input {
	if kind, _ := extKind(path); kind != "" {
		return kind
	}
	return sniffMedia(head)
}
