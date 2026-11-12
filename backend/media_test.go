package backend_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/backend"
	"github.com/deepnoodle-ai/decide/internal/mediatest"
)

// The documented way to test a provider's media rules: Config.BaseURL
// pointed at an httptest.Server. A file the provider takes arrives in its
// wire form; one it cannot take never arrives.
func TestMediaThroughBaseURL(t *testing.T) {
	png := mediatest.PNG(2, 2)
	pdf := decide.Media{MIMEType: "application/pdf", Data: mediatest.PDF()}
	for _, p := range []struct {
		provider backend.Provider
		account  string
		answer   string
		pdf      bool // takes PDFs
	}{
		{backend.Cloudflare, "acct", `{"success":true,"errors":[],"result":{"answers":{"ok":{"type":"noul","noul":0.9}}}}`, false},
		{backend.OpenAI, "", `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"ok","probability":0.9}]}`, false},
		{backend.Gemini, "", `{"answers":{"ok":{"type":"noul","noul":0.9,"confidence":0.9}}}`, true},
		{backend.TypeSafe, "", `{"answers":{"ok":{"noul":0.9}}}`, false},
	} {
		t.Run(string(p.provider), func(t *testing.T) {
			var hits atomic.Int32
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				io.WriteString(w, p.answer)
			}))
			defer srv.Close()
			client, err := backend.NewClient(backend.Config{Provider: p.provider, APIKey: "test-key-00000000",
				AccountID: p.account, BaseURL: srv.URL}, decide.WithMaxRetries(0))
			if err != nil {
				t.Fatal(err)
			}
			ask := func(m decide.Media) error {
				req := decide.NewRequest("a receipt", decide.WithMedia(m))
				req.Questions["ok"] = decide.Noul("Readable?")
				_, err := client.SystemOne(context.Background(), req, decide.WithCallValidation(false))
				return err
			}
			err = ask(decide.Media{MIMEType: "image/png", Data: png})
			if p.provider == backend.TypeSafe {
				if !errors.Is(err, errors.ErrUnsupported) {
					t.Fatalf("image: err = %v", err)
				}
			} else if err != nil || !strings.Contains(body, base64.StdEncoding.EncodeToString(png)) {
				t.Fatalf("image: err = %v, body = %.200s", err, body)
			}
			sent := hits.Load()
			err = ask(pdf)
			if p.pdf {
				if err != nil || hits.Load() != sent+1 {
					t.Fatalf("pdf: err = %v", err)
				}
			} else if !errors.Is(err, errors.ErrUnsupported) || hits.Load() != sent {
				t.Fatalf("pdf: err = %v, hits = %d", err, hits.Load()-sent)
			}
		})
	}
}
