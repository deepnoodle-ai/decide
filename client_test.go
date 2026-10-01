package decide_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

const envKey = "env-key-12345678"

func clearEnv(t *testing.T) {
	for _, k := range []string{"TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL", "TYPESAFE_LOG_LEVEL"} {
		t.Setenv(k, "")
	}
}

func TestEnvironment(t *testing.T) {
	srv := decidetest.NewServer(t, decidetest.WithAPIKey(envKey))
	ctx := t.Context()

	t.Run("env used", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", "  "+envKey+"\n")
		t.Setenv("TYPESAFE_BASE_URL", srv.URL)
		t.Setenv("TYPESAFE_DEFAULT_MODEL", "jev-preview")
		t.Setenv("TYPESAFE_LOG_LEVEL", "ERROR")
		c, err := decide.NewClient()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.SystemOne(ctx, valid()); err != nil {
			t.Fatal(err)
		}
		recs := srv.Requests()
		if last := recs[len(recs)-1]; last.Request.Model != "jev-preview" || last.Authorization != "Bearer "+envKey {
			t.Fatalf("model %q auth %q", last.Request.Model, last.Authorization)
		}
	})
	t.Run("options win", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", "wrong-key-12345678")
		t.Setenv("TYPESAFE_BASE_URL", "http://127.0.0.1:1")
		t.Setenv("TYPESAFE_DEFAULT_MODEL", "jev-preview")
		t.Setenv("TYPESAFE_LOG_LEVEL", "nonsense")
		c, err := decide.NewClient(decide.WithAPIKey(envKey), decide.WithBaseURL(srv.URL),
			decide.WithModel("jev-1.13.0"), decide.WithLogger(discardLogger()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.SystemOne(ctx, valid()); err != nil {
			t.Fatal(err)
		}
		recs := srv.Requests()
		if got := recs[len(recs)-1].Request.Model; got != "jev-1.13.0" {
			t.Fatalf("model %q", got)
		}
	})
	t.Run("blank env ignored", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", "   ")
		if _, err := decide.NewClient(); !errors.Is(err, decide.ErrNoAPIKey) {
			t.Fatalf("err %v", err)
		}
		t.Setenv("TYPESAFE_API_KEY", envKey)
		t.Setenv("TYPESAFE_BASE_URL", " ")
		t.Setenv("TYPESAFE_DEFAULT_MODEL", " ")
		t.Setenv("TYPESAFE_LOG_LEVEL", " ")
		c, err := decide.NewClient()
		if err != nil || !strings.Contains(c.String(), "model: jev-latest") || !strings.Contains(c.String(), "https://api.typesafe.ai") {
			t.Fatalf("defaults: %v %v", c, err)
		}
	})
	t.Run("WithoutEnvironment", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", envKey)
		t.Setenv("TYPESAFE_BASE_URL", srv.URL)
		t.Setenv("TYPESAFE_DEFAULT_MODEL", "jev-preview")
		t.Setenv("TYPESAFE_LOG_LEVEL", "nonsense")
		if _, err := decide.NewClient(decide.WithoutEnvironment()); !errors.Is(err, decide.ErrNoAPIKey) {
			t.Fatalf("err %v", err)
		}
		c, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey(envKey))
		if err != nil || !strings.Contains(c.String(), "model: jev-latest") || !strings.Contains(c.String(), "https://api.typesafe.ai") {
			t.Fatalf("env leaked: %v %v", c, err)
		}
	})
	t.Run("bad log level", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", envKey)
		t.Setenv("TYPESAFE_LOG_LEVEL", "verbose")
		if _, err := decide.NewClient(); err == nil {
			t.Fatal("want error")
		}
		for _, lvl := range []string{"debug", "Info", "WARN", "error", "off"} {
			t.Setenv("TYPESAFE_LOG_LEVEL", lvl)
			if _, err := decide.NewClient(); err != nil {
				t.Errorf("%s: %v", lvl, err)
			}
		}
	})
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestNewClientErrors(t *testing.T) {
	tr := answering(nil)
	cases := map[string][]decide.ClientOption{
		"no key":           {decide.WithoutEnvironment()},
		"empty key":        {decide.WithAPIKey("")},
		"negative retries": {decide.WithAPIKey(envKey), decide.WithMaxRetries(-1)},
		"initial > max":    {decide.WithAPIKey(envKey), decide.WithRetryBackoff(2, 1)},
		"negative timeout": {decide.WithAPIKey(envKey), decide.WithAttemptTimeout(-1)},
		"relative base":    {decide.WithAPIKey(envKey), decide.WithBaseURL("/v1")},
		"ftp base":         {decide.WithAPIKey(envKey), decide.WithBaseURL("ftp://x")},
		"empty base":       {decide.WithAPIKey(envKey), decide.WithBaseURL("")},
		"nil transport":    {decide.WithTransport(nil)},
		"nil http client":  {decide.WithAPIKey(envKey), decide.WithHTTPClient(nil)},
		"nil logger":       {decide.WithAPIKey(envKey), decide.WithLogger(nil)},
		"empty model":      {decide.WithAPIKey(envKey), decide.WithModel("")},
		"transport+key":    {decide.WithTransport(tr), decide.WithAPIKey(envKey)},
		"transport+base":   {decide.WithTransport(tr), decide.WithBaseURL("https://x")},
		"transport+http":   {decide.WithTransport(tr), decide.WithHTTPClient(http.DefaultClient)},
		"transport+agent":  {decide.WithTransport(tr), decide.WithUserAgent("app/1")},
	}
	for name, opts := range cases {
		if _, err := decide.NewClient(opts...); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	clearEnv(t)
	if _, err := decide.NewClient(); !errors.Is(err, decide.ErrNoAPIKey) {
		t.Errorf("ErrNoAPIKey: %v", err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	if _, err := decide.NewClient(decide.WithTransport(tr)); err != nil {
		t.Errorf("transport needs no key: %v", err)
	}
}

func TestBaseURLAndHeaders(t *testing.T) {
	var mu sync.Mutex
	var got []*http.Request
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Clone(t.Context()))
		mu.Unlock()
		w.Header().Set("x-typesafe-request-id", "req_h")
		w.Header().Set("x-extra", "1")
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"models":[{"name":"jev-latest","description":"d","release_date":"2026-09-01"}],"object":"list"}`))
			return
		}
		w.Write([]byte(`{"model":"jev-1.13.0","answers":{"n":{"type":"noul","noul":0.5}},"usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer hs.Close()
	c, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey(envKey),
		decide.WithBaseURL(hs.URL+"/proxy/decide//"), decide.WithUserAgent("myapp/1.2"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.SystemOne(t.Context(), valid())
	if err != nil {
		t.Fatal(err)
	}
	if resp.RequestID != "req_h" || resp.Header.Get("x-extra") != "1" || len(resp.Raw) == 0 || resp.Usage.InputTokens != 3 {
		t.Fatalf("response fields: %+v", resp)
	}
	list, err := c.Models.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := decide.Model{Name: "jev-latest", Description: "d", ReleaseDate: "2026-09-01"}
	if len(list.Models) != 1 || list.Models[0].Name != want.Name || list.Models[0].ReleaseDate != want.ReleaseDate ||
		list.RequestID != "req_h" || list.Header.Get("x-extra") != "1" || len(list.Raw) == 0 || string(list.Extra["object"]) != `"list"` {
		t.Fatalf("model list: %+v", list)
	}

	post, get := got[0], got[1]
	if post.URL.Path != "/proxy/decide/v1/systemone" || post.Method != http.MethodPost || get.URL.Path != "/proxy/decide/v1/models" {
		t.Fatalf("paths %s %s", post.URL.Path, get.URL.Path)
	}
	wantHeaders := map[string]string{
		"Authorization":      "Bearer " + envKey,
		"Accept":             "application/json",
		"Content-Type":       "application/json",
		"User-Agent":         "myapp/1.2 decide-go/" + decide.Version,
		"X-Typesafe-Sdk":     "decide-go/" + decide.Version,
		"X-Typesafe-Runtime": "go/" + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
	}
	for k, v := range wantHeaders {
		if post.Header.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, post.Header.Get(k), v)
		}
	}
	if get.Header.Get("Content-Type") != "" || get.Header.Get("X-Typesafe-Retry-Count") != "" {
		t.Errorf("GET headers %v", get.Header)
	}
}

func TestConcurrentSystemOne(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("tone", decidetest.ChoiceAnswer(map[string]float64{"calm": 0.8, "angry": 0.2}))
	c := srv.Client(t)
	req := decide.NewRequest("shared request, sent concurrently")
	tone := decide.Ask(req, "tone", decide.Choice("Tone?", decide.Option("calm"), decide.Option("angry")))
	decide.Ask(req, "urgency", decide.Score("Urgency?", "low", "high"))

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for range 50 {
		wg.Go(func() {
			resp, err := c.SystemOne(t.Context(), req)
			if err != nil {
				errs <- err
				return
			}
			if a, err := tone.From(resp); err != nil || a.Choice != "calm" {
				errs <- errors.Join(err, errors.New("wrong answer"))
			}
			if _, err := c.Models.List(t.Context()); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := len(srv.Requests()); n != 100 {
		t.Fatalf("%d requests", n)
	}
}

const (
	secretKey  = "sk-test-SECRET123"
	secretPart = "SECRET123"
)

func TestKeyNeverPrinted(t *testing.T) {
	srv := decidetest.NewServer(t, decidetest.WithAPIKey(secretKey))
	logger, logs := debugLogger()
	c := srv.Client(t, decide.WithLogger(logger), decide.WithLogBodies(true), decide.WithMaxRetries(1))
	cfg := decide.HTTPTransportConfig{APIKey: secretKey, BaseURL: srv.URL}
	tr, err := decide.NewHTTPTransport(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var seen []string
	check := func(what, s string) {
		t.Helper()
		seen = append(seen, s)
		if strings.Contains(s, secretPart) {
			t.Errorf("%s leaks the key: %s", what, s)
		}
	}

	// fmt verbs and slog LogValuer on every type that holds the key.
	// A custom transport that holds a key and has no String method: the
	// client must print it by type only.
	custom := stubClientTransport{apiKey: secretKey}
	cc, err := decide.NewClient(decide.WithTransport(custom))
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{
		"Client": c, "custom-transport Client": cc,
		"*HTTPTransport": tr, "HTTPTransport": *tr,
		"HTTPTransportConfig": cfg, "*HTTPTransportConfig": &cfg,
	} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			check(name+" "+verb, fmt.Sprintf(verb, v))
		}
		var text, js bytes.Buffer
		slog.New(slog.NewTextHandler(&text, nil)).Info("x", "v", v)
		slog.New(slog.NewJSONHandler(&js, nil)).Info("x", "v", v)
		check(name+" slog text", text.String())
		check(name+" slog json", js.String())
	}

	// Errors, with the key echoed in the body, from 401, 422, and 500.
	echo := []byte(`{"detail":{"error_type":"authentication_error","message":"bad key ` + secretKey + `"}}`)
	for _, status := range []int{401, 422, 500} {
		srv.FailNext(status, decidetest.FaultBody(echo))
		srv.FailNext(status, decidetest.FaultBody(echo)) // the 500 is retried once
		_, err := c.SystemOne(t.Context(), valid())
		if err == nil {
			t.Fatalf("%d: no error", status)
		}
		check(fmt.Sprintf("%d Error()", status), err.Error())
		check(fmt.Sprintf("%d %%+v", status), fmt.Sprintf("%+v %#v", err, err))
		if ae, ok := errors.AsType[*decide.APIError](err); ok {
			check(fmt.Sprintf("%d Body", status), string(ae.Body))
			check(fmt.Sprintf("%d Message", status), ae.Message)
			if !bytes.Contains(ae.Body, []byte("[REDACTED]")) {
				t.Errorf("%d: body not redacted: %s", status, ae.Body)
			}
		}
		srv.Reset()
	}

	// A real 401 from a wrong key, and a success whose body echoes the key.
	bad := srv.Client(t, decide.WithAPIKey("sk-test-WRONG-SECRET123"), decide.WithLogger(logger), decide.WithLogBodies(true))
	_, err = bad.SystemOne(t.Context(), valid())
	check("wrong key", fmt.Sprintf("%v %+v", err, bad))
	srv.Respond(func(req *decide.Request) (*decide.Response, error) {
		return &decide.Response{
			Answers: map[string]decide.Answer{"n": decidetest.NoulAnswer(0.5)},
			Extra:   map[string]json.RawMessage{"echo": json.RawMessage(`"` + secretKey + `"`)},
		}, nil
	})
	resp, err := c.SystemOne(t.Context(), valid())
	if err != nil {
		t.Fatal(err)
	}
	check("success Raw", string(resp.Raw))
	list, err := c.Models.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	check("model list", fmt.Sprintf("%+v", list))

	check("debug logs", logs.String())
	if !strings.Contains(logs.String(), "response_body=") || !strings.Contains(logs.String(), "level=DEBUG") {
		t.Fatalf("expected debug logs with bodies, got:\n%s", logs.String())
	}
	if len(seen) < 30 {
		t.Fatalf("only %d strings checked", len(seen))
	}
}

// stubClientTransport is a custom transport that holds a key.
type stubClientTransport struct {
	stubTransport
	apiKey string
}

func TestShortKeyNotSubstituted(t *testing.T) {
	const short = "abcd"
	srv := decidetest.NewServer(t, decidetest.WithAPIKey(short))
	body := []byte(`{"detail":"abcd is not a real key"}`)
	srv.FailNext(401, decidetest.FaultBody(body))
	_, err := srv.Client(t, decide.WithMaxRetries(0)).SystemOne(t.Context(), valid())
	ae, ok := errors.AsType[*decide.APIError](err)
	if !ok || !bytes.Equal(ae.Body, body) {
		t.Fatalf("short key body changed: %v", err)
	}
}
