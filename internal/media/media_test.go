package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

func pngOf(w, h int) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)))
	return buf.Bytes()
}

// webpHeader builds a WebP container with one image header chunk.
func webpHeader(kind string, w, h int) []byte {
	var chunk []byte
	switch kind {
	case "VP8X":
		chunk = make([]byte, 10)
		for i := range 3 {
			chunk[4+i], chunk[7+i] = byte((w-1)>>(8*i)), byte((h-1)>>(8*i))
		}
	case "VP8L":
		chunk = make([]byte, 5)
		chunk[0] = 0x2f
		binary.LittleEndian.PutUint32(chunk[1:], uint32(w-1)|uint32(h-1)<<14)
	case "VP8 ":
		chunk = make([]byte, 10)
		copy(chunk[3:], []byte{0x9d, 0x01, 0x2a})
		binary.LittleEndian.PutUint16(chunk[6:], uint16(w))
		binary.LittleEndian.PutUint16(chunk[8:], uint16(h))
	}
	data := make([]byte, 20+len(chunk)+len(chunk)%2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WEBP"+kind)
	binary.LittleEndian.PutUint32(data[16:], uint32(len(chunk)))
	copy(data[20:], chunk)
	return data
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Image/PNG":                 "image/png",
		"image/jpg":                 "image/jpeg",
		" IMAGE/JPEG ; quality=9 ":  "image/jpeg",
		"audio/x-wav":               "audio/wav",
		"audio/wave":                "audio/wav",
		"audio/vnd.wave":            "audio/wav",
		"audio/x-m4a":               "audio/m4a",
		"audio/mp4":                 "audio/m4a",
		"video/quicktime":           "video/mov",
		"audio/mp4a-latm":           "audio/m4a",
		"audio/x-aac":               "audio/aac",
		"audio/x-flac":              "audio/flac",
		"audio/x-aiff":              "audio/aiff",
		"application/ogg":           "audio/ogg",
		"video/x-m4v":               "video/mp4",
		"video/x-msvideo":           "video/avi",
		"video/vnd.avi":             "video/avi",
		"video/x-ms-wmv":            "video/wmv",
		"application/PDF":           "application/pdf",
		"text/plain; charset=utf-8": "text/plain",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKind(t *testing.T) {
	for in, want := range map[string]string{
		"image/png": Image, "image/x-anything": Image, "audio/wav": Audio, "video/mp4": Video,
		"application/pdf": PDF, "application/json": "", "text/plain": "", "image/": "", "image": "",
	} {
		if got := Kind(in); got != want {
			t.Errorf("Kind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContradicts(t *testing.T) {
	var jpg, gf bytes.Buffer
	_ = jpeg.Encode(&jpg, image.NewGray(image.Rect(0, 0, 2, 2)), nil)
	_ = gif.Encode(&gf, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil)
	mp4 := append([]byte{0, 0, 0, 0x18}, "ftypqt  "...)
	cases := []struct {
		t    string
		data []byte
		bad  bool
	}{
		{"image/png", pngOf(1, 1), false},
		{"image/jpeg", pngOf(1, 1), true},
		{"image/jpeg", jpg.Bytes(), false},
		{"image/gif", gf.Bytes(), false},
		{"image/webp", webpHeader("VP8L", 2, 2), false},
		{"image/png", []byte("not a png"), true},
		{"image/heic", mp4, false},
		{"video/mov", mp4, false},
		{"video/mp4", mp4, false},
		{"audio/m4a", mp4, false},
		{"application/pdf", []byte("%PDF-1.7\n"), false},
		{"application/pdf", pngOf(1, 1), true},
		{"audio/wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), false},
		{"audio/mpeg", []byte("ID3\x04"), false},
		{"audio/mp3", []byte{0xff, 0xfb, 0x90}, false},
		{"audio/ogg", []byte("OggS\x00"), false},
		{"audio/flac", []byte("fLaC"), false},
		{"video/webm", []byte("\x1a\x45\xdf\xa3"), false},
		{"audio/aiff", []byte("FORM...."), false}, // not recognized: taken as declared
		{"video/mp4", []byte("%PDF-1.4"), true},
		// Bytes of a known format and a type of the same kind: the
		// provider's type list decides.
		{"audio/x-unknown", []byte("fLaC"), false},
		{"audio/m4a", []byte("\x1a\x45\xdf\xa3"), false},
		{"image/x-anything", pngOf(1, 1), false},
		{"application/pdf", []byte("fLaC"), true},
		{"audio/flac", []byte("%PDF-1.4"), true},
		{"image/png", []byte("fLaC"), true},
	}
	for _, c := range cases {
		if _, bad := Contradicts(c.t, c.data); bad != c.bad {
			t.Errorf("Contradicts(%q, %q) = %t, want %t", c.t, c.data[:min(8, len(c.data))], bad, c.bad)
		}
	}
}

// TestContradictsEveryRow checks every type of every signature row against
// that row's bytes, and against bytes of a format of another kind.
func TestContradictsEveryRow(t *testing.T) {
	var jpg, gf bytes.Buffer
	_ = jpeg.Encode(&jpg, image.NewGray(image.Rect(0, 0, 2, 2)), nil)
	_ = gif.Encode(&gf, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil)
	ftyp := func(brand string) []byte {
		return append([]byte{0, 0, 0, 0x14}, "ftyp"+brand+"\x00\x00\x02\x00isom"...)
	}
	samples := map[string][][]byte{
		"PNG":  {pngOf(1, 1)},
		"JPEG": {jpg.Bytes()},
		"GIF":  {gf.Bytes(), []byte("GIF87a\x01\x00\x01\x00")},
		"WebP": {webpHeader("VP8L", 2, 2)},
		"PDF":  {[]byte("%PDF-1.7\n")},
		"WAV":  {[]byte("RIFF\x24\x00\x00\x00WAVEfmt ")},
		"MP3":  {[]byte("ID3\x04\x00"), {0xff, 0xfb, 0x90}, {0xff, 0xf3, 0x90}, {0xff, 0xf2, 0x90}},
		"Ogg":  {[]byte("OggS\x00\x02")},
		"FLAC": {[]byte("fLaC\x00\x00\x00\x22")},
		"MP4":  {ftyp("isom"), ftyp("qt  "), ftyp("M4A "), ftyp("3gp4"), ftyp("heic"), ftyp("avif")},
		"WebM": {[]byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81")},
	}
	for _, s := range signatures {
		data, ok := samples[s.name]
		if !ok {
			t.Errorf("no sample for the %s row", s.name)
			continue
		}
		for _, typ := range s.types {
			for _, d := range data {
				if looks, bad := Contradicts(typ, d); bad || looks != s.name {
					t.Errorf("Contradicts(%q, %s bytes %q) = %q, %t; want %q, false", typ, s.name, d[:min(8, len(d))], looks, bad, s.name)
				}
			}
			// PDF bytes contradict every type that is not a PDF; FLAC
			// bytes contradict a PDF.
			other := samples["PDF"][0]
			if Kind(typ) == PDF {
				other = samples["FLAC"][0]
			}
			if _, bad := Contradicts(typ, other); !bad {
				t.Errorf("Contradicts(%q, %q) = false, want true", typ, other[:8])
			}
		}
	}
}

func TestDimensions(t *testing.T) {
	var jpg bytes.Buffer
	_ = jpeg.Encode(&jpg, image.NewGray(image.Rect(0, 0, 30, 20)), nil)
	cases := []struct {
		t    string
		data []byte
		w, h int
	}{
		{"image/png", pngOf(7, 3), 7, 3},
		{"image/jpeg", jpg.Bytes(), 30, 20},
		{"image/webp", webpHeader("VP8X", 5000, 4000), 5000, 4000},
		{"image/webp", webpHeader("VP8L", 300, 200), 300, 200},
		{"image/webp", webpHeader("VP8 ", 640, 480), 640, 480},
	}
	for _, c := range cases {
		w, h, err := Dimensions(c.t, c.data)
		if err != nil || w != c.w || h != c.h {
			t.Errorf("Dimensions(%s) = %d, %d, %v; want %d, %d", c.t, w, h, err, c.w, c.h)
		}
	}
	if _, _, err := Dimensions("image/png", jpg.Bytes()); err == nil {
		t.Error("a JPEG read as a PNG has dimensions")
	}
	for _, size := range []uint32{0xffffffff, 0x80000000, 1000} {
		data := webpHeader("VP8X", 1, 1)
		binary.LittleEndian.PutUint32(data[16:20], size)
		if _, _, err := Dimensions("image/webp", data); err == nil {
			t.Errorf("untrusted chunk size %d accepted", size)
		}
	}
}

func FuzzWebPDimensions(f *testing.F) {
	f.Add(webpHeader("VP8X", 1, 1))
	f.Add(webpHeader("VP8L", 9, 9))
	f.Add(webpHeader("VP8 ", 9, 9))
	f.Fuzz(func(t *testing.T, data []byte) {
		w, h, err := webpDimensions(data)
		if err == nil && (w <= 0 || h <= 0) {
			t.Fatalf("dimensions %dx%d with no error", w, h)
		}
	})
}

func TestIsYouTube(t *testing.T) {
	for in, want := range map[string]bool{
		"https://youtube.com/watch?v=x":     true,
		"https://www.youtube.com/watch":     true,
		"https://m.YouTube.com/x":           true,
		"https://youtu.be/x":                true,
		"https://notyoutube.com/x":          false,
		"https://youtube.com.example.com/x": false,
		"https://example.com/youtube.com":   false,
	} {
		u, _ := url.Parse(in)
		if got := IsYouTube(u); got != want {
			t.Errorf("IsYouTube(%s) = %t", in, got)
		}
	}
}

func TestAppendBase64(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(`"`)
	AppendBase64(&buf, []byte("hello, world"))
	buf.WriteString(`"`)
	if want := `"` + base64.StdEncoding.EncodeToString([]byte("hello, world")) + `"`; buf.String() != want {
		t.Fatalf("got %s, want %s", buf.String(), want)
	}
}

var cloudflare = Limits{
	Provider: "cloudflare", Takes: "PNG, JPEG, and WebP images, as bytes",
	Types:    []string{"image/png", "image/jpeg", "image/webp"},
	MaxFiles: 4, MaxFileBytes: 4 << 20, MaxPixels: 16_000_000, MaxTotalBytes: 8 << 20,
}

func TestCheckMessages(t *testing.T) {
	big := pngOf(1, 1)
	big = append(big, make([]byte, 5<<20)...)
	files := func(fs ...File) []File {
		for i := range fs {
			fs[i].Index = i
		}
		return fs
	}
	gem := Limits{
		Provider: "gemini", Takes: "images, audio, video, and PDFs",
		Types:    []string{"image/png", "application/pdf", "video/mp4"},
		URLKinds: []string{Image, Video, PDF},
		CheckURL: func(u *url.URL) string {
			if IsYouTube(u) {
				return "YouTube URLs"
			}
			return ""
		},
		MaxBase64: 2_000_000, MaxBase64PDF: 1_000_000, Decimal: true,
	}
	cases := []struct {
		l    Limits
		in   []File
		want string
	}{
		{cloudflare, files(File{MIME: "application/pdf", Data: []byte("%PDF")}),
			"cloudflare does not take PDFs (media[0], application/pdf); it takes PNG, JPEG, and WebP images, as bytes"},
		{cloudflare, files(File{MIME: "image/png", URL: "https://x.test/a.png"}),
			"cloudflare does not take media by URL (media[0]); attach the file's bytes in Data"},
		{cloudflare, files(File{MIME: "image/gif", Data: []byte("GIF89a")}),
			"cloudflare does not take image/gif (media[0]); it takes PNG, JPEG, and WebP images, as bytes"},
		{cloudflare, files(File{MIME: "image/png", Data: big}),
			"cloudflare takes images of at most 4 MiB (media[0] is 5.0 MiB, image/png)"},
		{cloudflare, files(File{MIME: "image/webp", Data: webpHeader("VP8X", 5000, 4000)}),
			"cloudflare takes images of at most 16 megapixels (media[0] is 5000x4000, image/webp)"},
		{cloudflare, files(File{MIME: "image/png", Data: pngOf(1, 1)}, File{MIME: "image/png", Data: pngOf(1, 1)},
			File{MIME: "image/png", Data: pngOf(1, 1)}, File{MIME: "image/png", Data: pngOf(1, 1)}, File{MIME: "image/png", Data: pngOf(1, 1)}),
			"cloudflare takes at most 4 files (the request has 5)"},
		{cloudflare, files(File{MIME: "image/png", Data: big[:3<<20]}, File{MIME: "image/png", Data: big[:3<<20]}, File{MIME: "image/png", Data: big[:3<<20]}),
			"cloudflare takes at most 8 MiB of files in all (the request has 9 MiB)"},
		{gem, files(File{MIME: "video/mp4", URL: "https://www.youtube.com/watch?v=1"}),
			"gemini does not take YouTube URLs (media[0], video/mp4)"},
		{gem, files(File{MIME: "image/png", Data: make([]byte, 600_000)}, File{MIME: "application/pdf", URL: "https://x.test/a.pdf"}),
			""},
		{gem, files(File{MIME: "image/png", Data: make([]byte, 600_000)}, File{MIME: "application/pdf", Data: make([]byte, 600_000)}),
			"gemini takes at most 1 MB of inline base64 when a PDF is inline (the request has 1.6 MB)"},
		{gem, files(File{MIME: "audio/wav", Data: []byte("RIFF")}),
			"gemini does not take audio (media[0], audio/wav); it takes images, audio, video, and PDFs"},
	}
	for _, c := range cases {
		err := c.l.Check(c.in)
		if c.want == "" {
			if err != nil {
				t.Errorf("Check: %v", err)
			}
			continue
		}
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("Check = %v, want ErrUnsupported", err)
			continue
		}
		if got := strings.TrimPrefix(err.Error(), "unsupported operation: "); got != c.want {
			t.Errorf("Check =\n %s\nwant\n %s", got, c.want)
		}
	}
}

func TestSize(t *testing.T) {
	for _, c := range []struct {
		n       int64
		decimal bool
		want    string
	}{{4 << 20, false, "4 MiB"}, {5452595, false, "5.2 MiB"}, {100_000_000, true, "100 MB"}, {50_500_000, true, "50.5 MB"}} {
		if got := Size(c.n, c.decimal); got != c.want {
			t.Errorf("Size(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
