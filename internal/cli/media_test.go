package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide/internal/template"
)

// fakeGemini answers every question yes and records each request body.
func fakeGemini(t *testing.T) *[]map[string]any {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		var all map[string]any
		_ = json.Unmarshal(raw, &all)
		mu.Lock()
		bodies = append(bodies, all)
		mu.Unlock()
		answers := map[string]any{}
		for k := range body.Questions {
			answers[k] = map[string]any{"type": "noul", "noul": 0.9, "confidence": 0.9}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GEMINI_BASE_URL", srv.URL+"/v1beta")
	t.Setenv("GEMINI_API_KEY", "gm-secret")
	return &bodies
}

func TestRunPDFTemplateOnGemini(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	bodies := fakeGemini(t)
	dir := t.TempDir()
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.pdf", "%PDF-1.4 invoice")
	write("fake.pdf", "not a pdf")
	write("notes.txt", "ignored")

	// With no --provider, a PDF template uses Gemini.
	out := runReal("run", "invoice-check", dir, "--no-cache")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Running invoice-check on 1 PDF · gemini gemini-decision-1", "fake.pdf (not a readable PDF)")
	if len(*bodies) != 1 {
		t.Fatalf("%d requests", len(*bodies))
	}
	raw, _ := json.Marshal((*bodies)[0]["input"])
	if !strings.Contains(string(raw), `"type":"document"`) || !strings.Contains(string(raw), `"mime_type":"application/pdf"`) {
		t.Fatalf("input = %s", raw)
	}
}

func TestMediaNeedsAProviderThatTakesIt(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "cf")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4"), 0o600)
	out := runReal("run", "invoice-check", dir, "--provider", "cloudflare")
	if out.code != 1 {
		t.Fatalf("exit %d", out.code)
	}
	contains(t, out.stderr, "invoice-check reads PDF files, which need the gemini provider", "--provider gemini")
	// A dry run stops the same way.
	if out := runReal("run", "invoice-check", dir, "--provider", "typesafe", "--dry-run"); out.code != 1 {
		t.Fatalf("dry run accepted a provider the run rejects: exit %d", out.code)
	}
}

func TestImageProviderFollowsCredentials(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	bodies := fakeGemini(t)
	dir := t.TempDir()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	_ = os.WriteFile(filepath.Join(dir, "r.png"), buf.Bytes(), 0o600)
	// Only Gemini has a key, so receipt-quality runs there, not on Cloudflare.
	out := runReal("run", "receipt-quality", dir, "--no-cache")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "· gemini gemini-decision-1")
	raw, _ := json.Marshal((*bodies)[0]["input"])
	if !strings.Contains(string(raw), `"type":"image"`) {
		t.Fatalf("input = %s", raw)
	}
}

func TestMediaLimitsFollowTheProvider(t *testing.T) {
	const mib = 1 << 20
	if mediaLimit("cloudflare", "image") != 4*mib || mediaLimit("openai", "image") != 50*mib {
		t.Fatal("Clef keeps its 4 MiB limit, and OpenAI stops at 50 MiB")
	}
	for _, tc := range []struct {
		provider, input string
		base64          int64
	}{
		{"gemini", "image", 100_000_000},
		{"gemini", "video", 100_000_000},
		{"gemini", "pdf", 50_000_000},
	} {
		n := mediaLimit(tc.provider, templateInput(tc.input))
		if n <= 4*mib || (n+mib)*4/3 > tc.base64 {
			t.Errorf("%s %s: %d bytes does not fit %d of base64", tc.provider, tc.input, n, tc.base64)
		}
	}
}

func templateInput(s string) template.Input { return template.Input(s) }

func TestMediaProvidersWaitLongerForUploads(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("GEMINI_API_KEY", "gm")
	t.Setenv("OPENAI_API_KEY", "sk")
	t.Setenv("TYPESAFE_API_KEY", "ts")
	for provider, want := range map[string]string{"gemini": "5m0s", "openai": "5m0s", "typesafe": "10s"} {
		c, err := connect(provider, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(c.String(), "attempt_timeout: "+want+",") {
			t.Errorf("%s: %s", provider, c.String())
		}
	}
}

func TestMediaRunsSendTwoAtOnce(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	var mu sync.Mutex
	inFlight, most := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		io.WriteString(w, `{"answers":{"invoice":{"type":"noul","noul":0.9,"confidence":0.9},
			"complete":{"type":"noul","noul":0.9,"confidence":0.9},"paid":{"type":"noul","noul":0.1,"confidence":0.9}}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GEMINI_BASE_URL", srv.URL+"/v1beta")
	t.Setenv("GEMINI_API_KEY", "gm")
	dir := t.TempDir()
	for i := range 8 {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.pdf", i)), []byte(fmt.Sprintf("%%PDF-1.4 %d", i)), 0o600)
	}
	if out := runReal("run", "invoice-check", dir, "--no-cache"); out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	if most != 2 {
		t.Fatalf("%d requests at once, want 2", most)
	}
	most = 0
	if out := runReal("run", "invoice-check", dir, "--no-cache", "--workers", "4"); out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	if most != 4 {
		t.Fatalf("--workers 4 sent %d at once", most)
	}
}
