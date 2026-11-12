package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/backend"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/template"
)

// mediaSetup is setup with a fake server whose clients accept media, as
// a provider that reads media does. The harness gives every provider
// this client, so the walk's checks follow --provider and the requests
// are recorded.
func mediaSetup(t *testing.T) *harness {
	t.Helper()
	h := setup(t)
	h.server = decidetest.NewServer(t, decidetest.AcceptMedia())
	return h
}

// mediaRequests splits the recorded requests into those with and without
// media.
func mediaRequests(h *harness) (withMedia []decide.Media, without int) {
	for _, r := range h.server.Requests() {
		if r.Request == nil {
			continue
		}
		if len(r.Request.Media) == 0 {
			without++
		}
		withMedia = append(withMedia, r.Request.Media...)
	}
	return withMedia, without
}

// runDirs lists the saved runs.
func runDirs(t *testing.T) []string {
	t.Helper()
	dirs, _ := filepath.Glob(filepath.Join(os.Getenv("DECIDE_HOME"), "runs", "2*"))
	return dirs
}

// savedRun reads the newest saved run's run.json.
func savedRun(t *testing.T) map[string]any {
	t.Helper()
	dirs := runDirs(t)
	if len(dirs) == 0 {
		t.Fatal("no run saved")
	}
	slices.Sort(dirs)
	raw, err := os.ReadFile(filepath.Join(dirs[len(dirs)-1], "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// UC-1: a screenshot of a diff, judged by code-risk on a provider that
// reads images.
func TestRunTextTemplateOnNamedImage(t *testing.T) {
	h := mediaSetup(t)
	h.write("diff.png", pngOf(t, 2, 2))

	out := h.run("", "run", "code-risk", "diff.png", "--provider", "openai", "--dry-run")
	if out.code != 0 {
		t.Fatalf("dry run: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "code-risk would look at 1 image", "diff.png", "Nothing was sent")
	if n := h.requests(); n != 0 {
		t.Fatalf("dry run sent %d requests", n)
	}

	out = h.run("", "run", "code-risk", "diff.png", "--provider", "openai")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Running code-risk on 1 image · openai gpt-6-luna")
	reqs := h.server.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	req := reqs[0].Request
	if len(req.Media) != 1 || req.Media[0].MIMEType != "image/png" {
		t.Fatalf("media = %v", req.Media)
	}
	state, _ := json.Marshal(req.State)
	if string(state) != `{"content_type":"image/png","path":"diff.png"}` {
		t.Fatalf("state = %s", state)
	}
	if _, ok := req.Questions["risk"]; !ok {
		t.Fatalf("questions = %v", req.Questions)
	}
	if got := savedRun(t)["media_items"]; got != 1.0 {
		t.Fatalf("media_items = %v", got)
	}
	out = h.run("", "runs", "view")
	if out.code != 0 {
		t.Fatalf("view: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "diff.png")
}

// UC-2: the default provider reads text only.
func TestNamedImageOnTypeSafe(t *testing.T) {
	h := mediaSetup(t)
	h.write("diff.png", pngOf(t, 2, 2))
	out := h.run("", "run", "code-risk", "diff.png")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Error: diff.png is an image, and TypeSafe reads text only",
		"Hint: Images run on Cloudflare, OpenAI, or Gemini.\nRun it with --provider cloudflare, --provider openai, or --provider gemini")
	if n := h.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
	if dirs := runDirs(t); len(dirs) != 0 {
		t.Fatalf("a run was saved: %v", dirs)
	}

	// The same error in a dry run, with no key set.
	clearProviderEnv(t)
	out = runReal("run", "code-risk", filepath.Join(h.dir, "diff.png"), "--dry-run")
	contains(t, out.stderr, "diff.png is an image, and TypeSafe reads text only")

	// DECIDE_PROVIDER chooses the provider as --provider does.
	t.Setenv("DECIDE_PROVIDER", "openai")
	out = h.run("", "run", "code-risk", "diff.png")
	if out.code != 0 {
		t.Fatalf("DECIDE_PROVIDER=openai: exit %d: %s", out.code, out.stderr)
	}
	if media, _ := mediaRequests(h); len(media) != 1 {
		t.Fatalf("media = %v", media)
	}
}

// R-3: a provider that does not read a file's kind is the reason, before
// its format, since a new format alone would not help. The format fix
// follows "as well".
func TestProviderBeforeFormat(t *testing.T) {
	h := mediaSetup(t)
	h.write("photo.gif", "GIF89a\x01\x00\x01\x00\x80\x00\x00")
	h.write("shot", pngOf(t, 1, 1))
	h.write("scan", asciiPDF)
	h.write("shot.png", pngOf(t, 1, 1))
	images := "Images run on Cloudflare, OpenAI, or Gemini.\nRun it with --provider cloudflare, --provider openai, or --provider gemini"

	out := h.run("", "run", "code-risk", "photo.gif")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Error: photo.gif is an image, and TypeSafe reads text only\n",
		"Hint: "+images+"\nSave it as PNG, JPEG, or WebP as well.\n")
	lacks(t, out.stderr, "decide sends images only as")

	out = h.run("", "run", "code-risk", "shot")
	contains(t, out.stderr, "Error: shot is an image, and TypeSafe reads text only\n",
		"Hint: "+images+"\nName it with its extension, such as shot.png, as well.\n")

	out = h.run("", "run", "code-risk", "scan", "--provider", "openai")
	contains(t, out.stderr, "Error: scan is a PDF, and OpenAI reads images, not PDFs\n",
		"Hint: PDFs run on Gemini.\nRun it with --provider gemini\nName it with its extension, such as scan.pdf, as well.\n")

	// A provider that reads images: the format is the reason, as before.
	out = h.run("", "run", "code-risk", "photo.gif", "--provider", "openai")
	contains(t, out.stderr, "Error: photo.gif is an image, and decide sends images only as PNG, JPEG, or WebP",
		"Save it as PNG, JPEG, or WebP, and run decide on that file.")
	lacks(t, out.stderr, "OpenAI reads")

	// A GIF with no extension: a new name would not change its format,
	// so the hint says to save it, not to rename it.
	h.write("pic", "GIF89a\x01\x00\x01\x00\x80\x00\x00")
	out = h.run("", "run", "code-risk", "pic", "--provider", "openai")
	contains(t, out.stderr, "Error: pic is an image, and decide sends images only as PNG, JPEG, or WebP",
		"Save it as PNG, JPEG, or WebP, and run decide on that file.")
	lacks(t, out.stderr, "extension")
	out = h.run("", "run", "code-risk", "pic")
	contains(t, out.stderr, "Error: pic is an image, and TypeSafe reads text only\n",
		"Hint: "+images+"\nSave it as PNG, JPEG, or WebP as well.\n")
	lacks(t, out.stderr, "extension")

	// UC-8: in a list, the provider hint shows once, then the format line.
	out = h.run("", "run", "sentiment", "photo.gif", "shot.png")
	contains(t, out.stderr, `Error: 2 files you named can't be sent
  photo.gif: an image, and TypeSafe reads text only
  shot.png: an image, and TypeSafe reads text only

Hint: `+images+`
Save photo.gif as PNG, JPEG, or WebP as well.
`)
	if n := h.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

// UC-3: the chosen provider reads images, not audio or PDFs.
func TestNamedMediaKindProviderCannotRead(t *testing.T) {
	h := mediaSetup(t)
	h.write("call.mp3", "ID3\x03\x00\x00\x00\x00\x00\x00")
	h.write("report.pdf", asciiPDF)
	out := h.run("", "run", "sentiment", "call.mp3", "--provider", "openai")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Error: call.mp3 is an audio file, and OpenAI reads images, not audio",
		"Hint: Audio runs on Gemini.\nRun it with --provider gemini")
	out = h.run("", "run", "sentiment", "report.pdf", "--provider", "cloudflare")
	contains(t, out.stderr, "Error: report.pdf is a PDF, and Cloudflare reads images, not PDFs",
		"Hint: PDFs run on Gemini.\nRun it with --provider gemini")
	if n := h.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

// UC-4: text and an image in one run.
func TestMixedRun(t *testing.T) {
	h := mediaSetup(t)
	h.write("notes.txt", "great\nbad\nok\n")
	h.write("shot.png", pngOf(t, 2, 2))
	args := []string{"run", "sentiment", "notes.txt", "shot.png", "--provider", "gemini"}

	// What a dry run lists, with each unit, limit, and sample.
	items := func(extra ...string) []map[string]any {
		t.Helper()
		out := h.run("", append(append(slices.Clone(args), "--dry-run", "--json"), extra...)...)
		if out.code != 0 {
			t.Fatalf("%v: exit %d: %s", extra, out.code, out.stderr)
		}
		return jsonLines(t, out.stdout, true)
	}
	all := items()
	var units []string
	for _, it := range all {
		units = append(units, fmt.Sprint(it["unit"]))
	}
	if !slices.Equal(units, []string{"line", "line", "line", "image"}) {
		t.Fatalf("units = %v", units)
	}
	if got := items("--each", "file"); len(got) != 2 || got[1]["unit"] != "image" {
		t.Fatalf("--each file: %v", got)
	}
	if got := items("--limit", "2"); len(got) != 2 || got[1]["source"] != "notes.txt:2" {
		t.Fatalf("--limit 2: %v", got)
	}
	if got := items("--sample", "2"); len(got) != 2 {
		t.Fatalf("--sample 2: %v", got)
	}
	out := h.run("", append(slices.Clone(args), "--dry-run")...)
	contains(t, out.stdout, "sentiment would look at 3 lines and 1 image")

	// The run: three requests without media and one with.
	out = h.run("", append(slices.Clone(args), "--json")...)
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Running sentiment on 3 lines and 1 image · gemini gemini-decision-1")
	if lines := jsonLines(t, out.stdout, false); len(lines) != 4 {
		t.Fatalf("%d JSON lines", len(lines))
	}
	media, without := mediaRequests(h)
	if len(media) != 1 || media[0].MIMEType != "image/png" || without != 3 {
		t.Fatalf("media %v, %d without", media, without)
	}
	if got := savedRun(t)["media_items"]; got != 1.0 {
		t.Fatalf("media_items = %v", got)
	}

	// A second run answers every item from the cache.
	before := h.requests()
	out = h.run("", args...)
	if out.code != 0 || h.requests() != before {
		t.Fatalf("rerun: exit %d, %d new requests: %s", out.code, h.requests()-before, out.stderr)
	}
	// New bytes in the image ask about the image only.
	h.write("shot.png", pngOf(t, 3, 3))
	h.run("", args...)
	if n := h.requests() - before; n != 1 {
		t.Fatalf("%d requests after the image changed, want 1", n)
	}

	// A failed image resumes from the run's copy, after the file is gone.
	h.server.FailNext(422)
	out = h.run("", "run", "sentiment", "shot.png", "notes.txt", "--provider", "gemini", "--workers", "1", "--no-cache")
	if out.code != 1 {
		t.Fatalf("exit %d, want a failed item: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "✓ 3 answered", "✗ 1 failed")
	if err := os.Remove(filepath.Join(h.dir, "shot.png")); err != nil {
		t.Fatal(err)
	}
	h.server.Reset()
	out = h.run("", "runs", "resume")
	if out.code != 0 {
		t.Fatalf("resume: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "1 request left", "✓ 4 answered")
	if media, without := mediaRequests(h); len(media) != 1 || without != 0 {
		t.Fatalf("resume sent media %v and %d without", media, without)
	}
}

// UC-5: a folder gives a text template text only, on every provider.
func TestFolderWithImagesStaysText(t *testing.T) {
	h := mediaSetup(t)
	h.write("main.go", "package main\n")
	h.write("docs/a.png", pngOf(t, 1, 1))
	h.write("docs/b.png", pngOf(t, 1, 1))
	for _, provider := range []string{"typesafe", "openai"} {
		h.server.Reset()
		out := h.run("", "run", "code-risk", ".", "--provider", provider)
		if out.code != 0 {
			t.Fatalf("%s: exit %d: %s", provider, out.code, out.stderr)
		}
		contains(t, out.stderr, "Skipped 2 files that are not text: docs/a.png, docs/b.png\n", "Running code-risk on 1 file")
		lacks(t, out.stderr, "name them", "--provider "+provider+"\n")
		if media, without := mediaRequests(h); len(media) != 0 || without != 1 {
			t.Fatalf("%s: media %v, %d without", provider, media, without)
		}
	}

	// A folder of images only says how to send them.
	out := h.run("", "run", "code-risk", "docs", "--provider", "openai", "--dry-run")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Found nothing for code-risk to look at in docs",
		"Hint: In a folder, code-risk reads only text. To send the images, name them:\ndecide run code-risk docs/*.png --provider openai")
	// The UC-5 hint replaces the old one, so only one hint prints.
	lacks(t, out.stderr, "code-risk reads text.", "need a template that reads them")
	if n := strings.Count(out.stderr, "Hint:"); n != 1 {
		t.Fatalf("%d hints: %s", n, out.stderr)
	}
	out = h.run("", "run", "code-risk", "docs", "--dry-run")
	contains(t, out.stderr, "decide run code-risk docs/*.png --provider cloudflare")

	// A folder name that needs quotes is quoted, so the command can be
	// pasted.
	h.write("my shots/a.png", pngOf(t, 1, 1))
	out = h.run("", "run", "sentiment", "my shots", "--provider", "gemini", "--dry-run")
	contains(t, out.stderr, "decide run sentiment 'my shots'/*.png --provider gemini")

	// Each extension gets its own pattern, so none of the files is left out.
	// A PDF among them needs a provider that reads both kinds.
	h.write("mixed/a.png", pngOf(t, 1, 1))
	h.write("mixed/b.jpg", "\xff\xd8\xff\xe0")
	h.write("mixed/sub/c.pdf", asciiPDF)
	out = h.run("", "run", "sentiment", "mixed", "--provider", "openai", "--dry-run")
	contains(t, out.stderr, "To send the images and PDFs, name them:\ndecide run sentiment mixed/*.png mixed/*.jpg mixed/sub/*.pdf --provider gemini")
	lacks(t, out.stderr, "name the rest")

	// A folder outside the working directory: the pattern names the folder
	// on disk, not its short label, so the command resolves from here.
	outside := filepath.Join(t.TempDir(), "shots")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "a.png"), []byte(pngOf(t, 1, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out = h.run("", "run", "sentiment", outside, "--provider", "openai", "--dry-run")
	glob := shellQuote(filepath.ToSlash(outside)) + "/*.png"
	contains(t, out.stderr, "decide run sentiment "+glob+" --provider openai")
	if matches, _ := filepath.Glob(filepath.Join(outside, "*.png")); len(matches) != 1 {
		t.Fatalf("the pattern matches %v", matches)
	}

	// More than three patterns: the first three, and a note.
	h.write("many/a.png", pngOf(t, 1, 1))
	h.write("many/b.jpg", "\xff\xd8\xff\xe0")
	h.write("many/c.jpeg", "\xff\xd8\xff\xe0")
	h.write("many/d.webp", "RIFF\x00\x00\x00\x00WEBP")
	out = h.run("", "run", "sentiment", "many", "--provider", "openai", "--dry-run")
	contains(t, out.stderr, "decide run sentiment many/*.png many/*.jpg many/*.jpeg --provider openai\nThat is 3 of 4 folders and extensions; name the rest the same way.")
}

// UC-6: a media template takes a named media file of another kind.
func TestMediaTemplateOtherKind(t *testing.T) {
	h := mediaSetup(t)
	h.write("receipt.pdf", asciiPDF)
	h.write("photos/a.png", pngOf(t, 1, 1))
	h.write("photos/b.pdf", asciiPDF)
	out := h.run("", "run", "receipt-quality", "receipt.pdf", "--provider", "gemini")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Running receipt-quality on 1 PDF")
	if media, _ := mediaRequests(h); len(media) != 1 || media[0].MIMEType != "application/pdf" {
		t.Fatalf("media = %v", media)
	}

	// With only a Cloudflare key, receipt-quality runs on Cloudflare.
	clearProviderEnv(t)
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	out = runReal("run", "receipt-quality", filepath.Join(h.dir, "receipt.pdf"), "--dry-run")
	contains(t, out.stderr, "receipt.pdf is a PDF, and Cloudflare reads images, not PDFs", "Run it with --provider gemini")

	// A folder still gives it images only.
	out = h.run("", "run", "receipt-quality", "photos", "--provider", "gemini", "--dry-run")
	if out.code != 0 {
		t.Fatalf("folder: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "receipt-quality would look at 1 image", "photos/a.png")
	lacks(t, out.stdout, "b.pdf")
}

// UC-7: a media template on a folder, with a provider that cannot read
// its kind, stops before the walk.
func TestMediaTemplateFolderWrongProvider(t *testing.T) {
	h := mediaSetup(t)
	h.write("invoices/a.pdf", asciiPDF)
	h.write("scan.png", pngOf(t, 1, 1))
	for _, provider := range []string{"typesafe", "openai"} {
		for _, dry := range []bool{false, true} {
			args := []string{"run", "invoice-check", "invoices", "--provider", provider}
			if dry {
				args = append(args, "--dry-run")
			}
			out := h.run("", args...)
			if out.code != 1 {
				t.Fatalf("%v: exit %d: %s", args, out.code, out.stderr)
			}
			contains(t, out.stderr, "Error: invoice-check reads PDF files, which need the gemini provider", "Run it with --provider gemini")
			lacks(t, out.stderr, "Skipped")
		}
	}
	out := h.run("", "run", "invoice-check", "scan.png", "--provider", "openai")
	if out.code != 0 {
		t.Fatalf("scan.png: exit %d: %s", out.code, out.stderr)
	}
	if media, _ := mediaRequests(h); len(media) != 1 || media[0].MIMEType != "image/png" {
		t.Fatalf("media = %v", media)
	}
}

// UC-8: every named file that cannot be sent, in one error.
func TestSeveralNamedFilesFail(t *testing.T) {
	h := mediaSetup(t)
	h.write("call.mp3", "ID3\x03\x00\x00\x00\x00\x00\x00")
	h.write("intro.mp3", "ID3\x03\x00\x00\x00\x00\x00\x00")
	h.write("photo.gif", "GIF89a\x01\x00\x01\x00\x80\x00\x00")
	h.write("shot.png", pngOf(t, 1, 1))
	out := h.run("", "run", "sentiment", "call.mp3", "intro.mp3", "photo.gif", "shot.png", "--provider", "openai", "--limit", "1")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, `Error: 3 files you named can't be sent
  call.mp3: an audio file, and OpenAI reads images, not audio
  intro.mp3: an audio file, and OpenAI reads images, not audio
  photo.gif: an image, and decide sends images only as PNG, JPEG, or WebP

Hint: Audio runs on Gemini.
Run it with --provider gemini
Save photo.gif as PNG, JPEG, or WebP, and run decide on that file.`)
	lacks(t, out.stderr, "shot.png")
	if n := h.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}

	// At most 10 lines, then how many more.
	var names []string
	for i := range 12 {
		name := fmt.Sprintf("p%02d.gif", i)
		h.write(name, "GIF89a")
		names = append(names, name)
	}
	out = h.run("", append([]string{"run", "sentiment", "--provider", "openai"}, names...)...)
	contains(t, out.stderr, "12 files you named can't be sent", "  p09.gif: ", "\n  and 2 more\n")
	lacks(t, out.stderr, "p10.gif:")
}

// A named file the format of which decide does not send, a media file with
// no extension, and a text file for a media template.
func TestNamedFormatErrors(t *testing.T) {
	h := mediaSetup(t)
	h.write("shot", pngOf(t, 1, 1))
	h.write("notes.txt", "a receipt, typed out\n")
	out := h.run("", "run", "sentiment", "shot", "--provider", "openai")
	contains(t, out.stderr, "Error: shot is an image, and its name has no extension decide reads, such as .png",
		"Name it with its extension, such as shot.png, and run decide on that file.")
	lacks(t, out.stderr, "decide sends images only as")
	// In a list, the hint names the file.
	h.write("call", "ID3\x03\x00\x00\x00\x00\x00\x00")
	out = h.run("", "run", "sentiment", "shot", "call", "--provider", "gemini")
	contains(t, out.stderr, "  shot: an image, and its name has no extension decide reads, such as .png",
		"  call: an audio file, and its name has no extension decide reads, such as .mp3",
		"Name shot with its extension, such as shot.png", "Name call with its extension, such as call.mp3")
	out = h.run("", "run", "receipt-quality", "notes.txt", "--provider", "openai")
	contains(t, out.stderr, "notes.txt is not an image, audio, video, or PDF file, and receipt-quality reads images",
		"See the templates that read text with: decide templates")
	// A text file with no extension is text, not a media file to rename.
	h.write("LICENSE", "MIT License\n\nPermission is hereby granted.\n")
	out = h.run("", "run", "receipt-quality", "LICENSE", "--provider", "openai")
	if out.code != 1 {
		t.Fatalf("LICENSE: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Error: LICENSE is not an image, audio, video, or PDF file, and receipt-quality reads images",
		"See the templates that read text with: decide templates")
	lacks(t, out.stderr, "extension", "decide sends images only as")
	if n := h.requests(); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

// The table of which provider reads which kind agrees with the providers'
// transports: a file the table says a provider does not read is rejected
// before anything is sent, and one it reads is sent.
func TestMediaTableMatchesTransports(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"test server"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	files := map[template.Input]decide.Media{
		template.Image: {MIMEType: "image/png", Data: []byte(pngOf(t, 1, 1))},
		template.Audio: {MIMEType: "audio/wav", Data: []byte("RIFF\x24\x00\x00\x00WAVEfmt ")},
		template.Video: {MIMEType: "video/mp4", Data: []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")},
		template.PDF:   {MIMEType: "application/pdf", Data: []byte(asciiPDF)},
	}
	for _, provider := range []string{"typesafe", "cloudflare", "openai", "gemini"} {
		cfg := backend.Config{Provider: backend.Provider(provider), APIKey: "test-key", BaseURL: srv.URL}
		if provider == "cloudflare" {
			cfg.AccountID = "acct"
		}
		client, err := backend.NewClient(cfg, decide.WithMaxRetries(0))
		if err != nil {
			t.Fatal(err)
		}
		reads := mediaOptions(provider)
		for _, kind := range mediaKinds {
			req := decide.NewRequest("state", decide.WithMedia(files[kind]))
			decide.Ask(req, "q", decide.Noul("q"))
			_, err := client.SystemOne(context.Background(), req)
			_, want := reads[kind]
			if got := !errors.Is(err, errors.ErrUnsupported); got != want {
				t.Errorf("%s, %s: reads = %v, table says %v (%v)", provider, kind, got, want, err)
			}
		}
	}
}
