package cli

import (
	"bytes"
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngOf returns a PNG image of w by h pixels.
func pngOf(t *testing.T, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

const (
	asciiPDF    = "%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\nBT (Invoice 42) Tj ET\n%%EOF\n"
	corruptJPEG = "\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xff\xd9"
)

// noNUL is random bytes with no NUL byte.
var noNUL = func() string {
	r := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, 4000)
	for i := range b {
		b[i] = byte(1 + r.IntN(255))
	}
	return string(b)
}()

// Use case 1: a PDF given to a text template.
func TestTextTemplateRejectsAPDF(t *testing.T) {
	h := setup(t)
	h.write("report.pdf", asciiPDF)
	h.write("nonul.bin", noNUL)
	out := h.run("", "run", "sentiment", "report.pdf")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "report.pdf is a PDF, and sentiment reads text",
		"Images, PDFs, audio, and video need a template that reads them", "decide templates show invoice-check")
	out = h.run("", "run", "sentiment", "nonul.bin", "--each", "file")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "nonul.bin is not text, and sentiment reads text",
		"decide reads text, such as .txt, .md, .csv, .jsonl, and source files.")
	lacks(t, out.stderr, "Images, PDFs")
	if n := len(h.server.Requests()); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

// Use case 2: a dry run over a folder lists only text files, and warns
// once about the rest.
func TestDryRunListsOnlyText(t *testing.T) {
	h := setup(t)
	h.write("mixed/notes.md", "# Notes\n\nSome text.\n")
	h.write("mixed/main.go", "package main\n")
	h.write("mixed/a.pdf", asciiPDF)
	h.write("mixed/a.png", pngOf(t, 1, 1))
	h.write("mixed/nonul.bin", noNUL)
	h.write("mixed/a.docx", "PK\x03\x04[Content_Types].xml")
	out := h.run("", "run", "code-risk", "mixed", "--dry-run")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "mixed/notes.md", "mixed/main.go")
	lacks(t, out.stdout, "a.pdf", "a.png", "nonul.bin", "a.docx")
	if n := strings.Count(out.stderr, "Skipped"); n != 1 {
		t.Fatalf("%d warnings:\n%s", n, out.stderr)
	}
	contains(t, out.stderr, "Skipped 4 files that are not text: mixed/a.docx, mixed/a.pdf, mixed/a.png, and 1 more")

	// A folder with no text says which templates read the rest.
	h.write("scans/a.pdf", asciiPDF)
	out = h.run("", "run", "code-risk", "scans", "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Found nothing for code-risk to look at in scans",
		"code-risk reads text. Images, PDFs, audio, and video need a template that reads them.")
}

// Use case 3: an image piped to a text template.
func TestTextTemplateRejectsAnImageOnStdin(t *testing.T) {
	h := setup(t)
	out := h.run(pngOf(t, 1, 1), "run", "sentiment")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "stdin is an image, and sentiment reads text", "decide templates show receipt-quality")
	out = h.run(noNUL, "run", "sentiment")
	contains(t, out.stderr, "stdin is not text, and sentiment reads text")
	if n := len(h.server.Requests()); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

// Use case 4: corrupt JPEGs among good PNGs are skipped, and the good ones
// are answered.
func TestCorruptImagesDoNotStopARun(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	bodies := fakeGemini(t)
	dir := t.TempDir()
	for _, name := range []string{"c1.jpg", "c2.jpg", "c3.jpg", "c4.jpg", "c5.jpg", "c6.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(corruptJPEG), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"y-good.png", "z-good.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(pngOf(t, 2, 2)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := runReal("run", "receipt-quality", dir, "--no-cache", "--workers", "1")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	if n := strings.Count(out.stderr, "Skipped"); n != 1 {
		t.Fatalf("%d warnings:\n%s", n, out.stderr)
	}
	contains(t, out.stderr, "Skipped 6 files (not a readable image): ", "c3.jpg, and 3 more",
		"Running receipt-quality on 2 images", "✓ 2 answered")
	lacks(t, out.stderr, "Stopped because")
	if len(*bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(*bodies))
	}
}

// Use case 5: an image over Cloudflare's limit names Cloudflare and the
// providers that take it.
func TestTooLargeNamesTheProvider(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	dir := t.TempDir()
	big := filepath.Join(dir, "photos", "big.png")
	if err := os.MkdirAll(filepath.Dir(big), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, []byte(pngOf(t, 1, 1)+strings.Repeat("\x00", 5<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runReal("run", "receipt-quality", big)
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "big.png is 5.0 MiB, and Cloudflare takes image files up to 4 MiB",
		"Gemini takes up to", "OpenAI takes up to 50 MiB", "--provider gemini or --provider openai")

	// In a folder, the file is skipped, and the hint is about size, not
	// about formats.
	out = runReal("run", "receipt-quality", filepath.Dir(big), "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Skipped 1 file (over 4 MiB, the most Cloudflare takes for one image file): photos/big.png",
		"Found nothing for receipt-quality", "--provider gemini")
	lacks(t, out.stderr, "This template reads PNG")
}

func TestMediaTemplateKeyHint(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "ts")
	dir := t.TempDir()
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte(pngOf(t, 1, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runReal("run", "receipt-quality", path)
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "receipt-quality reads images, which TypeSafe does not take",
		"CLOUDFLARE_AUTH_TOKEN", "OPENAI_API_KEY", "GEMINI_API_KEY", "--dry-run")
	// A provider chosen with --provider keeps its own message.
	out = runReal("run", "receipt-quality", path, "--provider", "openai")
	contains(t, out.stderr, "OPENAI_API_KEY is not set")
	lacks(t, out.stderr, "TypeSafe does not take")
}

func TestMediaTemplateFlagsAndStdin(t *testing.T) {
	h := setup(t)
	h.write("a.png", pngOf(t, 1, 1))
	for _, flag := range []string{"--field", "--items"} {
		out := h.run("", "run", "receipt-quality", "a.png", flag, "body", "--dry-run")
		if out.code != 1 {
			t.Fatalf("%s: exit %d", flag, out.code)
		}
		contains(t, out.stderr, "receipt-quality reads one image file at a time, so "+flag+" does not apply")
	}
	out := h.run(pngOf(t, 1, 1), "run", "receipt-quality", "--dry-run")
	if out.code != 1 {
		t.Fatalf("stdin: exit %d", out.code)
	}
	contains(t, out.stderr, "receipt-quality reads image files, not stdin",
		"Name files or a folder: decide run receipt-quality photos")
}

func TestMediaHelpText(t *testing.T) {
	h := setup(t)
	out := h.run("", "templates", "new", "--help")
	contains(t, out.stdout, "(text, image, audio, video, or pdf)")
	out = h.run("", "run", "--help")
	contains(t, out.stdout, "for images, PDFs, audio, and video")
	lacks(t, out.stdout, "cloudflare for images")
	out = h.run("", "templates", "show", "invoice-check")
	contains(t, out.stdout, "Reads: PDF files, one item per file\n")
	out = h.run("")
	contains(t, out.stdout, "need a Cloudflare, OpenAI, or Gemini key instead")
}

// Text that is not UTF-8, such as a CSV saved by Excel as Windows-1252, is
// read as text. A binary file that no media template reads gets a hint
// about text, not about media templates.
func TestTextNotInUTF8(t *testing.T) {
	h := setup(t)
	h.write("l.csv", "id,review\n1,Caf\xe9\n")
	out := h.run("", "run", "sentiment", "l.csv", "--dry-run")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "l.csv:2")
	lacks(t, out.stderr, "not text")
	out = h.run("id,review\n1,Caf\xe9\n", "run", "sentiment", "--dry-run")
	if out.code != 0 {
		t.Fatalf("stdin: exit %d: %s", out.code, out.stderr)
	}

	for name, hint := range map[string]string{
		"a.docx": "Save it as plain text or Markdown",
		"a.xlsx": "Save it as CSV",
		"a.zip":  "Unpack it",
	} {
		h.write(name, "PK\x03\x04[Content_Types].xml")
		out := h.run("", "run", "sentiment", name)
		if out.code != 1 {
			t.Fatalf("%s: exit %d: %s", name, out.code, out.stderr)
		}
		contains(t, out.stderr, name+" is not text, and sentiment reads text", hint)
		lacks(t, out.stderr, "Images, PDFs", "receipt-quality")
	}

	// A folder of documents with nothing to read says what decide reads.
	h.write("docs/a.docx", "PK\x03\x04")
	out = h.run("", "run", "sentiment", "docs", "--dry-run")
	contains(t, out.stderr, "Found nothing for sentiment", "sentiment reads text, such as .txt")
	lacks(t, out.stderr, "Images, PDFs")
	if n := len(h.server.Requests()); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

// A named media file that fails a local check gets a sentence and a next
// step. An image over Cloudflare's pixel limit names the providers that
// take it.
func TestNamedMediaThatFailsACheck(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	dir := t.TempDir()
	write := func(name, data string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	out := runReal("run", "receipt-quality", write("c1.jpg", corruptJPEG), "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "c1.jpg is not a readable image",
		"Open it to check it, and save it again as PNG, JPEG, or WebP.",
		"In a folder, decide skips files like this: decide run receipt-quality photos")

	out = runReal("run", "receipt-quality", write("huge.png", pngOf(t, 5000, 4000)), "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "huge.png is 5000×4000 pixels, and Cloudflare takes images up to 16 megapixels",
		"OpenAI and Gemini take larger images.",
		"Run it with --provider openai or --provider gemini, or resize it to 16 megapixels or fewer.")

	t.Setenv("GEMINI_API_KEY", "g")
	out = runReal("run", "invoice-check", write("fake.pdf", "not a PDF"), "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "fake.pdf is not a readable PDF", "save it again as a PDF")
}

// A folder of images over Cloudflare's pixel limit gets a hint about
// pixels, not about bytes.
func TestPixelSkipsHaveTheirOwnHint(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "huge.png"), []byte(pngOf(t, 5000, 4000)), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runReal("run", "receipt-quality", dir, "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Found nothing for receipt-quality", "OpenAI and Gemini take larger images.",
		"resize it to 16 megapixels or fewer")
	lacks(t, out.stderr, "MiB", "This template reads PNG")

	// With a good image too, the hint is one dim line after the walk.
	if err := os.WriteFile(filepath.Join(dir, "good.png"), []byte(pngOf(t, 2, 2)), 0o600); err != nil {
		t.Fatal(err)
	}
	out = runReal("run", "receipt-quality", dir, "--dry-run")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "OpenAI and Gemini take larger images. Run it with --provider openai or --provider gemini")
	lacks(t, out.stderr, "MiB")
}
