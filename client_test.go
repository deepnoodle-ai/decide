package sod_test

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

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/sodtest"
)

const envKey = "env-key-12345678"

func clearEnv(t *testing.T) {
	for _, k := range []string{"TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL", "TYPESAFE_LOG_LEVEL"} {
		t.Setenv(k, "")
	}
}

func TestEnvironment(t *testing.T) {
	srv := sodtest.NewServer(t, sodtest.WithAPIKey(envKey))
	ctx := t.Context()

	t.Run("env used", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", "  "+envKey+"\n")
		t.Setenv("TYPESAFE_BASE_URL", srv.URL)
		t.Setenv("TYPESAFE_DEFAULT_MODEL", "jev-preview")
		t.Setenv("TYPESAFE_LOG_LEVEL", "ERROR")
		c, err := sod.NewClient()
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
		c, err := sod.NewClient(sod.WithAPIKey(envKey), sod.WithBaseURL(srv.URL),
			sod.WithModel("jev-1.13.0"), sod.WithLogger(discardLogger()))
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
		if _, err := sod.NewClient(); !errors.Is(err, sod.ErrNoAPIKey) {
			t.Fatalf("err %v", err)
		}
		t.Setenv("TYPESAFE_API_KEY", envKey)
		t.Setenv("TYPESAFE_BASE_URL", " ")
		t.Setenv("TYPESAFE_DEFAULT_MODEL", " ")
		t.Setenv("TYPESAFE_LOG_LEVEL", " ")
		c, err := sod.NewClient()
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
		if _, err := sod.NewClient(sod.WithoutEnvironment()); !errors.Is(err, sod.ErrNoAPIKey) {
			t.Fatalf("err %v", err)
		}
		c, err := sod.NewClient(sod.WithoutEnvironment(), sod.WithAPIKey(envKey))
		if err != nil || !strings.Contains(c.String(), "model: jev-latest") || !strings.Contains(c.String(), "https://api.typesafe.ai") {
			t.Fatalf("env leaked: %v %v", c, err)
		}
	})
	t.Run("bad log level", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("TYPESAFE_API_KEY", envKey)
		t.Setenv("TYPESAFE_LOG_LEVEL", "verbose")
		if _, err := sod.NewClient(); err == nil {
			t.Fatal("want error")
		}
		for _, lvl := range []string{"debug", "Info", "WARN", "error", "off"} {
			t.Setenv("TYPESAFE_LOG_LEVEL", lvl)
			if _, err := sod.NewClient(); err != nil {
				t.Errorf("%s: %v", lvl, err)
			}
		}
	})
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestNewClientErrors(t *testing.T) {
	tr := answering(nil)
	cases := map[string][]sod.ClientOption{
		"no key":           {sod.WithoutEnvironment()},
		"empty key":        {sod.WithAPIKey("")},
		"negative retries": {sod.WithAPIKey(envKey), sod.WithMaxRetries(-1)},
		"initial > max":    {sod.WithAPIKey(envKey), sod.WithRetryBackoff(2, 1)},
		"negative timeout": {sod.WithAPIKey(envKey), sod.WithAttemptTimeout(-1)},
		"relative base":    {sod.WithAPIKey(envKey), sod.WithBaseURL("/v1")},
		"ftp base":         {sod.WithAPIKey(envKey), sod.WithBaseURL("ftp://x")},
		"empty base":       {sod.WithAPIKey(envKey), sod.WithBaseURL("")},
		"nil transport":    {sod.WithTransport(nil)},
		"nil http client":  {sod.WithAPIKey(envKey), sod.WithHTTPClient(nil)},
		"nil logger":       {sod.WithAPIKey(envKey), sod.WithLogger(nil)},
		"empty model":      {sod.WithAPIKey(envKey), sod.WithModel("")},
		"transport+key":    {sod.WithTransport(tr), sod.WithAPIKey(envKey)},
		"transport+base":   {sod.WithTransport(tr), sod.WithBaseURL("https://x")},
		"transport+http":   {sod.WithTransport(tr), sod.WithHTTPClient(http.DefaultClient)},
		"transport+agent":  {sod.WithTransport(tr), sod.WithUserAgent("app/1")},
	}
	for name, opts := range cases {
		if _, err := sod.NewClient(opts...); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	clearEnv(t)
	if _, err := sod.NewClient(); !errors.Is(err, sod.ErrNoAPIKey) {
		t.Errorf("ErrNoAPIKey: %v", err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	if _, err := sod.NewClient(sod.WithTransport(tr)); err != nil {
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
	c, err := sod.NewClient(sod.WithoutEnvironment(), sod.WithAPIKey(envKey),
		sod.WithBaseURL(hs.URL+"/proxy/sod//"), sod.WithUserAgent("myapp/1.2"))
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
	want := sod.Model{Name: "jev-latest", Description: "d", ReleaseDate: "2026-09-01"}
	if len(list.Models) != 1 || list.Models[0].Name != want.Name || list.Models[0].ReleaseDate != want.ReleaseDate ||
		list.RequestID != "req_h" || list.Header.Get("x-extra") != "1" || len(list.Raw) == 0 || string(list.Extra["object"]) != `"list"` {
		t.Fatalf("model list: %+v", list)
	}

	post, get := got[0], got[1]
	if post.URL.Path != "/proxy/sod/v1/systemone" || post.Method != http.MethodPost || get.URL.Path != "/proxy/sod/v1/models" {
		t.Fatalf("paths %s %s", post.URL.Path, get.URL.Path)
	}
	wantHeaders := map[string]string{
		"Authorization":      "Bearer " + envKey,
		"Accept":             "application/json",
		"Content-Type":       "application/json",
		"User-Agent":         "myapp/1.2 sod-go/" + sod.Version,
		"X-Typesafe-Sdk":     "sod-go/" + sod.Version,
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
	srv := sodtest.NewServer(t)
	srv.Answer("tone", sodtest.ChoiceAnswer(map[string]float64{"calm": 0.8, "angry": 0.2}))
	c := srv.Client(t)
	req := sod.NewRequest("shared request, sent concurrently")
	tone := sod.Ask(req, "tone", sod.Choice("Tone?", sod.Option("calm"), sod.Option("angry")))
	sod.Ask(req, "urgency", sod.Score("Urgency?", "low", "high"))

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
	srv := sodtest.NewServer(t, sodtest.WithAPIKey(secretKey))
	logger, logs := debugLogger()
	c := srv.Client(t, sod.WithLogger(logger), sod.WithLogBodies(true), sod.WithMaxRetries(1))
	cfg := sod.HTTPTransportConfig{APIKey: secretKey, BaseURL: srv.URL}
	tr, err := sod.NewHTTPTransport(cfg)
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
	cc, err := sod.NewClient(sod.WithTransport(custom))
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
		srv.FailNext(status, sodtest.FaultBody(echo))
		srv.FailNext(status, sodtest.FaultBody(echo)) // the 500 is retried once
		_, err := c.SystemOne(t.Context(), valid())
		if err == nil {
			t.Fatalf("%d: no error", status)
		}
		check(fmt.Sprintf("%d Error()", status), err.Error())
		check(fmt.Sprintf("%d %%+v", status), fmt.Sprintf("%+v %#v", err, err))
		if ae, ok := errors.AsType[*sod.APIError](err); ok {
			check(fmt.Sprintf("%d Body", status), string(ae.Body))
			check(fmt.Sprintf("%d Message", status), ae.Message)
			if !bytes.Contains(ae.Body, []byte("[REDACTED]")) {
				t.Errorf("%d: body not redacted: %s", status, ae.Body)
			}
		}
		srv.Reset()
	}

	// A real 401 from a wrong key, and a success whose body echoes the key.
	bad := srv.Client(t, sod.WithAPIKey("sk-test-WRONG-SECRET123"), sod.WithLogger(logger), sod.WithLogBodies(true))
	_, err = bad.SystemOne(t.Context(), valid())
	check("wrong key", fmt.Sprintf("%v %+v", err, bad))
	srv.Respond(func(req *sod.Request) (*sod.Response, error) {
		return &sod.Response{
			Answers: map[string]sod.Answer{"n": sodtest.NoulAnswer(0.5)},
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
	srv := sodtest.NewServer(t, sodtest.WithAPIKey(short))
	body := []byte(`{"detail":"abcd is not a real key"}`)
	srv.FailNext(401, sodtest.FaultBody(body))
	_, err := srv.Client(t, sod.WithMaxRetries(0)).SystemOne(t.Context(), valid())
	ae, ok := errors.AsType[*sod.APIError](err)
	if !ok || !bytes.Equal(ae.Body, body) {
		t.Fatalf("short key body changed: %v", err)
	}
}
