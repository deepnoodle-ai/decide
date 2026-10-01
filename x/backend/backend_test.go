package backend_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/backend"
)

const testKey = "backend-test-key-00000000"

// fakeBoth uses the two actual HTTP dialects with the same canned answer.
func fakeBoth() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req decide.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, "invalid key", 401)
			return
		}
		response := &decide.Response{Model: req.Model, Answers: map[string]decide.Answer{"billing": decidetest.NoulAnswer(0.93)}}
		switch r.URL.Path {
		case "/v1/systemone":
			_ = json.NewEncoder(w).Encode(response)
		case "/accounts/account-1/ai/run/@cf/cloudflare/clef", "/accounts/account-1/ai/run/@cf/cloudflare/clef-flash":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "messages": []any{}, "result": response})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSwitchProviders(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "wrong-environment-key")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "wrong-environment-model")
	t.Setenv("TYPESAFE_BASE_URL", "invalid-environment-url")
	t.Setenv("TYPESAFE_LOG_LEVEL", "invalid-environment-level")
	srv := fakeBoth()
	defer srv.Close()
	req := decide.NewRequest("I was charged twice.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this about billing?"))
	for _, tc := range []struct {
		provider backend.Provider
		model    string
		want     string
	}{
		{backend.TypeSafe, "", "jev-latest"}, {backend.TypeSafe, "jev-pinned", "jev-pinned"},
		{backend.Cloudflare, "", "clef"}, {backend.Cloudflare, "clef-flash", "clef-flash"},
	} {
		cfg := backend.Config{Provider: tc.provider, APIKey: testKey, BaseURL: srv.URL, Model: tc.model}
		if tc.provider == backend.Cloudflare {
			cfg.AccountID = "account-1"
		}
		client, err := backend.NewClient(cfg, decide.WithMaxRetries(0), decide.WithAttemptTimeout(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.SystemOne(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		a, err := billing.From(resp)
		if err != nil || a.Noul != 0.93 || resp.Model != tc.want || req.Model != "" {
			t.Fatalf("backend %s: %+v %+v %v", tc.provider, resp, a, err)
		}
	}
}

func TestRequestModelOverridesBackendDefault(t *testing.T) {
	srv := fakeBoth()
	defer srv.Close()
	c, err := backend.NewClient(backend.Config{Provider: backend.Cloudflare, APIKey: testKey, AccountID: "account-1", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	req := decide.NewRequest("state", decide.WithRequestModel("clef-flash"))
	decide.Ask(req, "billing", decide.Noul("Billing?"))
	resp, err := c.SystemOne(context.Background(), req)
	if err != nil || resp.Model != "clef-flash" {
		t.Fatalf("per-request selector failed: %+v %v", resp, err)
	}
}

func TestInvalidConfigAndConnectionOptions(t *testing.T) {
	for _, cfg := range []backend.Config{
		{}, {Provider: "openai", APIKey: testKey}, {Provider: backend.TypeSafe},
		{Provider: backend.Cloudflare, APIKey: testKey},
		{Provider: backend.TypeSafe, APIKey: testKey, AccountID: "account-1"},
	} {
		if _, err := backend.NewClient(cfg); err == nil {
			t.Fatalf("invalid config accepted: %v", cfg)
		}
	}
	cfg := backend.Config{Provider: backend.TypeSafe, APIKey: testKey}
	for _, opt := range []decide.ClientOption{decide.WithAPIKey("other-key"), decide.WithBaseURL("https://example.com"), decide.WithHTTPClient(http.DefaultClient), decide.WithUserAgent("custom")} {
		if _, err := backend.NewClient(cfg, opt); err == nil {
			t.Fatal("native connection option accepted")
		}
	}
	if _, err := backend.NewClient(cfg, decide.WithMaxRetries(-1)); err == nil {
		t.Fatal("invalid shared option accepted")
	}
	if _, err := backend.NewClient(backend.Config{Provider: backend.Cloudflare, AccountID: "account-1"}); !errors.Is(err, decide.ErrNoAPIKey) {
		t.Fatalf("credential sentinel lost: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg), testKey) {
		t.Fatal("configuration leaked key")
	}
}

// One request and typed handle work with either backend. The server supplies
// canned answers, so this example runs without provider credentials.
func ExampleNewClient() {
	srv := fakeBoth()
	defer srv.Close()
	req := decide.NewRequest("I was charged twice.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this about billing?"))
	for _, provider := range []backend.Provider{backend.TypeSafe, backend.Cloudflare} {
		cfg := backend.Config{Provider: provider, APIKey: testKey, BaseURL: srv.URL}
		if provider == backend.Cloudflare {
			cfg.AccountID = "account-1"
		}
		client, err := backend.NewClient(cfg)
		if err != nil {
			panic(err)
		}
		resp, err := client.SystemOne(context.Background(), req)
		if err != nil {
			panic(err)
		}
		answer, err := billing.From(resp)
		if err != nil {
			panic(err)
		}
		fmt.Println(provider, resp.Model, answer.Noul)
	}
	// Output:
	// typesafe jev-latest 0.93
	// cloudflare clef 0.93
}
