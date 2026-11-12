package source

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/template"
)

// pngOf returns a PNG image of w by h pixels.
func pngOf(w, h int) string {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return b.String()
}

// A JPEG with a header and no image: the start, a JFIF segment, and the end.
const corruptJPEG = "\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xff\xd9"

const asciiPDF = "%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"

// noNUL is random bytes with no NUL byte, like part of a compressed file.
var noNUL = func() string {
	r := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, 4000)
	for i := range b {
		b[i] = byte(1 + r.IntN(255))
	}
	return string(b)
}()

// latin1CSV is text saved as Latin-1 or Windows-1252, which is not UTF-8.
const latin1CSV = "id,review\n1,Caf\xe9\n2,na\xefve but \x93good\x94\n"

func TestIsBinary(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       bool
	}{
		{"text", "package main\n", false},
		{"UTF-8 text", "café ☕ naïve\n", false},
		{"text cut inside a character", strings.Repeat("a", 7999) + "é", false},
		{"NUL", "a\x00b", true},
		{"Latin-1 text", latin1CSV, false},
		{"Latin-1 text, all of it", strings.Repeat("\xe9t\xe9 \xe0 Gen\xe8ve, ", 400), false},
		{"text with a few control characters", strings.Repeat("a line of text with one stray\x01 control character\n", 50), false},
		{"text with terminal colors", strings.Repeat("\x1b[31mred\x1b[0m\n", 100), false},
		{"random bytes with no NUL", noNUL, true},
		{"control characters, more than 1 in 20", strings.Repeat("\x01", 6) + strings.Repeat("a", 94), true},
		{"PDF", asciiPDF, true},
		{"PNG", pngOf(1, 1), true},
		{"zip", "PK\x03\x04[Content_Types].xml", true},
		{"ID3 audio", "ID3\x03\x00\x00", true},
		{"text that starts with ID3", "ID3 tags name the artist\n", false},
	} {
		if got := isBinary([]byte(tc.data)); got != tc.want {
			t.Errorf("%s: isBinary = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTextTemplatesSkipBinaryFiles(t *testing.T) {
	tree(t, map[string]string{
		"mixed/notes.md":    "# Notes\n",
		"mixed/a.pdf":       asciiPDF,
		"mixed/nonul.bin":   noNUL,
		"mixed/a.png":       pngOf(1, 1),
		"mixed/big.mp4":     strings.Repeat("x", MaxFileBytes+1), // skipped by its name, not its size
		"mixed/reviews.txt": "great\n",
		"mixed/latin1.csv":  latin1CSV,
	})
	items, warnings := walk(t, []string{"mixed"}, "", Options{Input: template.Text})
	if got := labels(items); !reflect.DeepEqual(got, []string{"mixed/latin1.csv:2", "mixed/latin1.csv:3", "mixed/notes.md", "mixed/reviews.txt:1"}) {
		t.Fatalf("labels = %v", got)
	}
	want := []string{"Skipped 4 files that are not text: mixed/a.pdf, mixed/a.png, mixed/big.mp4, and 1 more"}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings = %q", warnings)
	}
	for _, each := range []string{"", template.EachFile, template.EachLine} {
		for path, kind := range map[string]template.Input{"mixed/a.pdf": template.PDF, "mixed/a.png": template.Image, "mixed/big.mp4": template.Video} {
			err := Walk(context.Background(), []string{path}, nil, Options{Input: template.Text, Each: each}, func(Item) error {
				t.Fatalf("%s was read as text", path)
				return nil
			})
			var me *MediaError
			if !errors.As(err, &me) || me.Kind != kind || me.Label != path {
				t.Errorf("%s with --each %q: %v", path, each, err)
			}
		}
		err := Walk(context.Background(), []string{"mixed/nonul.bin"}, nil, Options{Input: template.Text, Each: each}, func(Item) error { return nil })
		var nt *NotTextError
		if !errors.As(err, &nt) {
			t.Errorf("nonul.bin with --each %q: %v", each, err)
		}
	}
}

func TestStdinThatIsNotText(t *testing.T) {
	for _, tc := range []struct {
		data string
		kind template.Input
	}{
		{pngOf(1, 1), template.Image},
		{asciiPDF, template.PDF},
		{noNUL, ""},
		{"PK\x03\x04[Content_Types].xml", ""},
	} {
		err := Walk(context.Background(), nil, strings.NewReader(tc.data), Options{Input: template.Text}, func(Item) error {
			t.Fatal("stdin was read as text")
			return nil
		})
		var me *MediaError
		var nt *NotTextError
		switch {
		case tc.kind != "" && (!errors.As(err, &me) || me.Kind != tc.kind || me.Label != "stdin"):
			t.Errorf("%s on stdin: %v", tc.kind, err)
		case tc.kind == "" && !errors.As(err, &nt):
			t.Errorf("binary on stdin: %v", err)
		}
	}
}

func TestMediaChecksInTheWalk(t *testing.T) {
	tree(t, map[string]string{
		"photos/c1.jpg":    corruptJPEG,
		"photos/c2.jpg":    corruptJPEG,
		"photos/gif.png":   "GIF89a\x01\x00\x01\x00\x80\x00\x00",
		"photos/empty.png": "",
		"photos/good.png":  pngOf(2, 2),
		"photos/huge.png":  pngOf(5000, 4000),
		"photos/a.heic":    "\x00\x00\x00\x18ftypheic",
		"photos/notes.txt": "text",
	})
	var skipped Skipped
	items, warnings := walk(t, []string{"photos"}, "", Options{Input: template.Image, MaxPixels: 16_000_000, Provider: "Cloudflare", Skipped: &skipped})
	if got := labels(items); !reflect.DeepEqual(got, []string{"photos/good.png"}) {
		t.Fatalf("labels = %v", got)
	}
	want := []string{
		"Skipped photos/c1.jpg (not a readable image)",
		"Skipped photos/c2.jpg (not a readable image)",
		"Skipped photos/empty.png (empty)",
		"Skipped photos/gif.png (not a PNG, JPEG, or WebP image)",
		"Skipped photos/huge.png (5000×4000 pixels; Cloudflare takes images up to 16 megapixels)",
		"Skipped 2 files that are not PNG, JPEG, or WebP",
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings = %q", warnings)
	}
	if skipped != (Skipped{Unsupported: 2, Invalid: 4, TooLarge: 1}) {
		t.Fatalf("skipped = %+v", skipped)
	}

	// A named file that fails a check is a typed error.
	for name, reason := range map[string]string{
		"photos/c1.jpg":    "not a readable image",
		"photos/empty.png": "empty",
		"photos/gif.png":   "not a PNG, JPEG, or WebP image",
	} {
		err := Walk(context.Background(), []string{name}, nil, Options{Input: template.Image}, func(Item) error { return nil })
		var ie *InvalidMediaError
		if !errors.As(err, &ie) || ie.Label != name || ie.Reason != reason || ie.Input != template.Image {
			t.Errorf("%s: %v", name, err)
		}
	}
	err := Walk(context.Background(), []string{"photos/huge.png"}, nil,
		Options{Input: template.Image, MaxPixels: 16_000_000, Provider: "Cloudflare"}, func(Item) error { return nil })
	var pe *PixelsError
	if !errors.As(err, &pe) || pe.Width != 5000 || pe.Height != 4000 || pe.Limit != 16_000_000 || pe.Provider != "Cloudflare" {
		t.Errorf("huge.png: %v", err)
	}
	tree(t, map[string]string{"fake.pdf": "not a PDF"})
	err = Walk(context.Background(), []string{"fake.pdf"}, nil, Options{Input: template.PDF}, func(Item) error { return nil })
	var ie *InvalidMediaError
	if !errors.As(err, &ie) || ie.Reason != "not a readable PDF" {
		t.Errorf("fake.pdf: %v", err)
	}
}

func TestMediaTooLarge(t *testing.T) {
	big := pngOf(1, 1) + strings.Repeat("\x00", 1<<20)
	tree(t, map[string]string{"dir/big.png": big})
	opts := Options{Input: template.Image, MaxMediaBytes: 1 << 20, Provider: "Cloudflare"}
	var skipped Skipped
	opts.Skipped = &skipped
	items, warnings := walk(t, []string{"dir"}, "", opts)
	if len(items) != 0 || skipped.TooLarge != 1 || len(warnings) != 1 ||
		warnings[0] != "Skipped dir/big.png (1.0 MiB; Cloudflare takes image files up to 1 MiB)" {
		t.Fatalf("items %v, skipped %+v, warnings %q", labels(items), skipped, warnings)
	}
	err := Walk(context.Background(), []string{"dir/big.png"}, nil, opts, func(Item) error { return nil })
	var tl *TooLargeError
	if !errors.As(err, &tl) || tl.Limit != 1<<20 || tl.Size != int64(len(big)) {
		t.Fatalf("named file: %v", err)
	}
}

func TestUnreadableFilesAreSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	for _, in := range []template.Input{template.Text, template.Image} {
		tree(t, map[string]string{"dir/a.png": pngOf(1, 1), "dir/b.png": pngOf(1, 1), "dir/a.md": "# a", "dir/b.md": "# b"})
		for _, name := range []string{"dir/a.png", "dir/a.md"} {
			if err := os.Chmod(name, 0); err != nil {
				t.Fatal(err)
			}
		}
		items, warnings := walk(t, []string{"dir"}, "", Options{Input: in})
		if len(items) != 1 {
			t.Fatalf("%s: labels = %v", in, labels(items))
		}
		found := false
		for _, w := range warnings {
			found = found || strings.Contains(w, "(could not read it: permission denied)")
		}
		if !found {
			t.Fatalf("%s: warnings = %q", in, warnings)
		}
	}
}

func TestSampledMediaHoldNoBytes(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d"} {
		files["v/"+name+".mp4"] = "\x00\x00\x00\x18ftypmp42" + strings.Repeat("\x00", 1<<16)
	}
	tree(t, files)
	items, _ := walk(t, []string{"v"}, "", Options{Input: template.Video, MaxMediaBytes: 1 << 20, Sample: 2})
	if len(items) != 2 {
		t.Fatalf("labels = %v", labels(items))
	}
	for _, it := range items {
		if it.Image == nil || it.Image.Path == "" || it.Image.Size != 1<<16+12 {
			t.Fatalf("image = %+v", it.Image)
		}
	}
}

func TestTextNotInUTF8IsRead(t *testing.T) {
	tree(t, map[string]string{"l.csv": latin1CSV})
	for _, stdin := range []bool{false, true} {
		var items []Item
		paths, in := []string{"l.csv"}, io.Reader(nil)
		if stdin {
			paths, in = nil, strings.NewReader(latin1CSV)
		}
		err := Walk(context.Background(), paths, in, Options{Input: template.Text}, func(it Item) error {
			items = append(items, it)
			return nil
		})
		if err != nil || len(items) < 2 || !strings.Contains(string(items[len(items)-1].State), "na\ufffdve") {
			t.Fatalf("stdin %v: %d items, %v", stdin, len(items), err)
		}
	}
}

func TestBinaryThatIsNotMedia(t *testing.T) {
	tree(t, map[string]string{"a.docx": "PK\x03\x04[Content_Types].xml", "blob": noNUL})
	for _, name := range []string{"a.docx", "blob"} {
		err := Walk(context.Background(), []string{name}, nil, Options{Input: template.Text}, func(Item) error { return nil })
		var nt *NotTextError
		if !errors.As(err, &nt) || nt.Label != name {
			t.Errorf("%s: %v", name, err)
		}
	}
	var skipped Skipped
	tree(t, map[string]string{"d/a.docx": "PK\x03\x04", "d/a.pdf": asciiPDF, "d/blob": noNUL})
	walk(t, []string{"d"}, "", Options{Input: template.Text, Skipped: &skipped})
	if skipped.NotText != 3 || skipped.NotTextMedia != 1 {
		t.Fatalf("skipped = %+v", skipped)
	}
}
