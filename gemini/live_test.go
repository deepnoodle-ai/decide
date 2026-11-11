//go:build live

// Live tests call the real Gemini Decisions API. Run with:
//
//	GEMINI_API_KEY=... go test -tags live -run Live ./gemini
package gemini_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/gemini"
)

func TestLiveAllPrimitives(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY is not set")
	}
	transport, err := gemini.NewTransport(gemini.Config{APIKey: os.Getenv("GEMINI_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithTransport(transport), decide.WithModel("gemini-decision-1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	req := decide.NewRequest("Help! My payouts have been failing for 3 days.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing or payouts?"))
	tone := decide.Ask(req, "tone", decide.Choice("What is the customer's tone?",
		decide.Option("calm"), decide.Option("frustrated"), decide.Option("angry")))
	urgency := decide.Ask(req, "urgency", decide.Score("How urgent is this?", "can wait", "this week", "today"))
	resp, err := client.SystemOne(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := billing.From(resp); err != nil || b.Noul < 0.5 {
		t.Errorf("billing %+v %v", b, err)
	}
	if c, err := tone.From(resp); err != nil || c.Choice == "calm" {
		t.Errorf("tone %+v %v", c, err)
	}
	if s, err := urgency.From(resp); err != nil || s.Score < 1 || s.Legend["2"] != "today" {
		t.Errorf("urgency %+v %v", s, err)
	}
	if resp.Usage.InputTokens == 0 {
		t.Errorf("response %+v", resp)
	}
}

// minimalPDF is a one-page PDF that shows text.
func minimalPDF(text string) []byte {
	stream := "BT /F1 24 Tf 72 700 Td (" + text + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return []byte(b.String())
}

func TestLiveMedia(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY is not set")
	}
	transport, err := gemini.NewTransport(gemini.Config{APIKey: os.Getenv("GEMINI_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithTransport(transport), decide.WithModel("gemini-decision-1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	red := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range red.Pix {
		if i%4 == 0 || i%4 == 3 {
			red.Pix[i] = 255
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, red)
	img, err := gemini.NewMedia(gemini.KindImage, "image/png", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gemini.NewMedia(gemini.KindDocument, "application/pdf", minimalPDF("INVOICE  Total due: $42.00"))
	if err != nil {
		t.Fatal(err)
	}
	req := decide.NewRequest("An image and a PDF are attached.")
	isRed := decide.Ask(req, "red", decide.Noul("Is the attached image solid red?"))
	invoice := decide.Ask(req, "invoice", decide.Noul("Is the attached PDF an invoice?"))
	if err := gemini.SetMedia(req, img, doc); err != nil {
		t.Fatal(err)
	}
	resp, err := client.SystemOne(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if a, err := isRed.From(resp); err != nil || a.Noul < 0.5 {
		t.Errorf("red %+v %v", a, err)
	}
	if a, err := invoice.From(resp); err != nil || a.Noul < 0.5 {
		t.Errorf("invoice %+v %v", a, err)
	}
}

// TestLiveAudioFormats sends a one-second tone in each audio format the
// CLI reads, to check that the API takes each MIME type. It needs ffmpeg.
func TestLiveAudioFormats(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY is not set")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	transport, err := gemini.NewTransport(gemini.Config{APIKey: os.Getenv("GEMINI_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithTransport(transport), decide.WithModel("gemini-decision-1"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range []struct{ ext, mime string }{
		{"mp3", "audio/mpeg"}, {"wav", "audio/wav"}, {"m4a", "audio/m4a"}, {"aac", "audio/aac"},
		{"flac", "audio/flac"}, {"ogg", "audio/ogg"}, {"opus", "audio/opus"}, {"aiff", "audio/aiff"},
		{"weba", "audio/webm"},
	} {
		t.Run(f.ext, func(t *testing.T) {
			path := filepath.Join(dir, "tone."+f.ext)
			args := []string{"-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}
			if f.ext == "weba" {
				args = append(args, "-c:a", "libopus", "-f", "webm")
			}
			if out, err := exec.Command(ffmpeg, append(args, path)...).CombinedOutput(); err != nil {
				t.Skipf("ffmpeg cannot write %s: %s", f.ext, out)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			m, err := gemini.NewMedia(gemini.KindAudio, f.mime, data)
			if err != nil {
				t.Fatal(err)
			}
			req := decide.NewRequest("")
			speech := decide.Ask(req, "speech", decide.Noul("Does the attached audio contain human speech?"))
			if err := gemini.SetMedia(req, m); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			resp, err := client.SystemOne(ctx, req)
			if err != nil {
				t.Fatalf("%s: %v", f.mime, err)
			}
			if a, err := speech.From(resp); err != nil || a.Noul > 0.5 {
				t.Errorf("a tone heard as speech: %+v %v", a, err)
			}
		})
	}
}

// TestLiveVideoFormats sends a one-second test pattern in each video format
// the CLI reads. It needs ffmpeg.
func TestLiveVideoFormats(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY is not set")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	transport, err := gemini.NewTransport(gemini.Config{APIKey: os.Getenv("GEMINI_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithTransport(transport), decide.WithModel("gemini-decision-1"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range []struct{ ext, mime string }{
		{"mp4", "video/mp4"}, {"mov", "video/mov"}, {"webm", "video/webm"}, {"mpg", "video/mpeg"},
		{"avi", "video/avi"}, {"wmv", "video/wmv"}, {"flv", "video/x-flv"}, {"3gp", "video/3gpp"},
	} {
		t.Run(f.ext, func(t *testing.T) {
			path := filepath.Join(dir, "pattern."+f.ext)
			args := []string{"-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=176x144:rate=25:duration=1"}
			if f.ext == "3gp" {
				args = append(args, "-c:v", "h263")
			}
			if out, err := exec.Command(ffmpeg, append(args, path)...).CombinedOutput(); err != nil {
				t.Skipf("ffmpeg cannot write %s: %s", f.ext, out)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			m, err := gemini.NewMedia(gemini.KindVideo, f.mime, data)
			if err != nil {
				t.Fatal(err)
			}
			req := decide.NewRequest("")
			people := decide.Ask(req, "people", decide.Noul("Does the attached video show people?"))
			if err := gemini.SetMedia(req, m); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			resp, err := client.SystemOne(ctx, req)
			if err != nil {
				t.Fatalf("%s: %v", f.mime, err)
			}
			if a, err := people.From(resp); err != nil || a.Noul > 0.5 {
				t.Errorf("a test pattern seen as people: %+v %v", a, err)
			}
		})
	}
}
