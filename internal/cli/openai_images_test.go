package cli

import (
	"bytes"
	"encoding/json"
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
)

func TestRunImageTemplateOnOpenAI(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("DECIDE_HOME", t.TempDir())
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		io.WriteString(w, `{"model":"gpt-6-luna","answers":[
			{"type":"predicate","name":"receipt","probability":0.9},
			{"type":"predicate","name":"readable","probability":0.8}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "sk-secret")

	dir := t.TempDir()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if err := os.WriteFile(filepath.Join(dir, "r.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runReal("run", "receipt-quality", dir, "--provider", "openai", "--no-cache")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	if len(bodies) != 1 {
		t.Fatalf("%d requests", len(bodies))
	}
	raw, _ := json.Marshal(bodies[0]["input"])
	if !strings.Contains(string(raw), `"image_url":"data:image/png;base64,`) || !strings.Contains(string(raw), `"input_image"`) {
		t.Fatalf("input = %s", raw)
	}
}
