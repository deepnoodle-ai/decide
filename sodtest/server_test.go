package sodtest_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/sodtest"
)

func request() *sod.Request {
	req := sod.NewRequest("I was double charged.")
	sod.Ask(req, "billing", sod.Noul("About billing?"))
	sod.Ask(req, "tone", sod.Choice("Tone?", sod.Option("calm"), sod.Option("angry")))
	sod.Ask(req, "urgency", sod.Score("Urgency?", "low", "mid", "high"))
	req.Questions["rank"] = &sod.RawQuestion{Type: "rank", JSON: json.RawMessage(`{"type":"rank","items":[1]}`)}
	return req
}

func TestStartWithoutTB(t *testing.T) {
	srv, err := sodtest.Start()
	if err != nil {
		t.Fatal(err)
	}
	c, err := srv.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SystemOne(t.Context(), request()); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	srv.Close() // idempotent
	if _, err := c.SystemOne(t.Context(), request(), sod.WithCallMaxRetries(0)); err == nil {
		t.Fatal("request after Close succeeded")
	}
}

func TestNewClientIgnoresEnv(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "env-key-should-not-be-used")
	t.Setenv("TYPESAFE_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "env-model")
	t.Setenv("TYPESAFE_LOG_LEVEL", "nonsense")
	srv := sodtest.NewServer(t)
	if _, err := srv.Client(t).SystemOne(t.Context(), request()); err != nil {
		t.Fatal(err)
	}
	if got := srv.Requests()[0].Request.Model; got != "jev-latest" {
		t.Fatalf("model %q", got)
	}
}

func TestDefaultAnswers(t *testing.T) {
	srv := sodtest.NewServer(t)
	resp, err := srv.Client(t).SystemOne(t.Context(), request())
	if err != nil {
		t.Fatal(err) // validation is on: defaults must validate
	}
	if resp.Model != "jev-1.13.0" || resp.RequestID != "req_1" {
		t.Fatalf("model %q id %q", resp.Model, resp.RequestID)
	}
	if n := resp.Answers["billing"].(*sod.NoulAnswer); n.Noul != 0.5 {
		t.Errorf("noul %v", n.Noul)
	}
	if c := resp.Answers["tone"].(*sod.ChoiceAnswer); c.Choice != "calm" || c.Probabilities["angry"] != 0.5 || c.Confidence != 0 {
		t.Errorf("choice %+v", c)
	}
	if s := resp.Answers["urgency"].(*sod.ScoreAnswer); s.Score != 1 || s.LevelLabel(2) != "high" {
		t.Errorf("score %+v", s)
	}
	if r := resp.Answers["rank"].(*sod.RawAnswer); r.Type != "rank" || string(r.JSON) != `{"type":"rank"}` {
		t.Errorf("raw %+v", r)
	}
	if resp.Usage.OutputTokens != 8 || resp.Usage.InputTokens == 0 {
		t.Errorf("usage %+v", resp.Usage)
	}
}

func TestCannedAnswersAndReset(t *testing.T) {
	srv := sodtest.NewServer(t)
	c := srv.Client(t)
	srv.Answer("billing", sodtest.NoulAnswer(0.9))
	resp, err := c.SystemOne(t.Context(), request())
	if err != nil || resp.Answers["billing"].(*sod.NoulAnswer).Noul != 0.9 {
		t.Fatalf("canned: %v", err)
	}
	srv.Reset()
	resp, _ = c.SystemOne(t.Context(), request())
	if resp.Answers["billing"].(*sod.NoulAnswer).Noul != 0.5 || len(srv.Requests()) != 1 {
		t.Fatal("Reset did not clear canned answers or records")
	}
}

func TestFaultsInOrder(t *testing.T) {
	fake := sodtest.NewServer(t)
	synctest.Test(t, func(t *testing.T) {
		ts := httptest.NewTestServer(t, fake.Handler())
		hc := ts.Client()
		c, err := fake.NewClientFor(ts.URL, hc, sod.WithMaxRetries(0))
		if err != nil {
			t.Fatal(err)
		}
		fake.FailNext(429, sodtest.FaultDelay(2*time.Second))
		fake.FailNext(503)
		fake.Overloaded(1)
		start := time.Now()
		for _, want := range []int{429, 503, 529} {
			_, err := c.SystemOne(t.Context(), request())
			if ae, ok := errors.AsType[*sod.APIError](err); !ok || ae.StatusCode != want {
				t.Fatalf("want %d, got %v", want, err)
			}
		}
		if d := time.Since(start); d != 2*time.Second {
			t.Fatalf("FaultDelay: %v", d)
		}
		if _, err := c.SystemOne(t.Context(), request()); err != nil {
			t.Fatalf("after faults: %v", err)
		}
	})
}

func TestFailNextConn(t *testing.T) {
	srv := sodtest.NewServer(t)
	srv.FailNextConn()
	res, err := http.Get(srv.URL + "/v1/models")
	if err == nil {
		res.Body.Close()
		t.Fatalf("got status %d, want a dropped connection", res.StatusCode)
	}
}

func TestResponderErrors(t *testing.T) {
	srv := sodtest.NewServer(t)
	c := srv.Client(t, sod.WithMaxRetries(0))
	srv.Respond(func(*sod.Request) (*sod.Response, error) { return nil, errors.New("boom") })
	_, err := c.SystemOne(t.Context(), request())
	ae, ok := errors.AsType[*sod.APIError](err)
	if !ok || ae.StatusCode != 500 || ae.Message != "boom" || string(ae.Body) != `{"detail":"boom"}` {
		t.Fatalf("plain error: %v", err)
	}
	srv.Respond(func(*sod.Request) (*sod.Response, error) {
		return nil, &sod.APIError{StatusCode: 418, Body: []byte(`{"detail":"teapot"}`)}
	})
	_, err = c.SystemOne(t.Context(), request())
	if ae, ok := errors.AsType[*sod.APIError](err); !ok || ae.StatusCode != 418 || ae.Message != "teapot" {
		t.Fatalf("APIError: %v", err)
	}
}

func TestAuth(t *testing.T) {
	srv := sodtest.NewServer(t)
	for _, tc := range []struct {
		auth string
		want int
	}{{"", 403}, {"Bearer wrong-key-000000", 401}, {"Bearer test-key-00000000", 200}} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/models", nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.want || res.Header.Get("x-typesafe-request-id") == "" {
			t.Errorf("%q: %d %s", tc.auth, res.StatusCode, body)
		}
		if tc.want != 200 && !strings.Contains(string(body), "authentication_error") {
			t.Errorf("%q: body %s", tc.auth, body)
		}
	}
	res, _ := http.Get(srv.URL + "/nope")
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Errorf("unauthenticated 404 path: %d", res.StatusCode)
	}
}

func TestBadBodies(t *testing.T) {
	srv := sodtest.NewServer(t)
	for _, body := range []string{`not json`, `{"state":"s","questions":{}}`} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/systemone", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key-00000000")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 422 || !strings.Contains(string(b), `"detail":[`) {
			t.Errorf("%s: %d %s", body, res.StatusCode, b)
		}
	}
}

func TestRecordedRequests(t *testing.T) {
	srv := sodtest.NewServer(t)
	c := srv.Client(t)
	c.SystemOne(t.Context(), request())
	c.Models.List(t.Context())
	recs := srv.Requests()
	if len(recs) != 2 {
		t.Fatalf("%d records", len(recs))
	}
	post, get := recs[0], recs[1]
	if post.Method != "POST" || post.Path != "/v1/systemone" || post.Request == nil || len(post.Request.Questions) != 4 ||
		post.Authorization != "Bearer test-key-00000000" || post.RequestID != "req_1" || len(post.Body) == 0 {
		t.Errorf("post %+v", post)
	}
	if _, ok := post.Request.Questions["tone"].(*sod.ChoiceQuestion); !ok {
		t.Errorf("decoded question %T", post.Request.Questions["tone"])
	}
	if get.Method != "GET" || get.Request != nil || get.RequestID != "req_2" {
		t.Errorf("get %+v", get)
	}
}

func TestWithModelsAndResolvedModel(t *testing.T) {
	srv := sodtest.NewServer(t, sodtest.WithResolvedModel("jev-9.9.9"),
		sodtest.WithModels(sod.Model{Name: "jev-x", ReleaseDate: "2026-01-01"}))
	c := srv.Client(t)
	list, err := c.Models.List(t.Context())
	if err != nil || len(list.Models) != 1 || list.Models[0].Name != "jev-x" {
		t.Fatalf("%+v %v", list, err)
	}
	resp, err := c.SystemOne(t.Context(), request())
	if err != nil || resp.Model != "jev-9.9.9" {
		t.Fatalf("%v %v", resp, err)
	}
}

func TestFixtures(t *testing.T) {
	q := sod.Choice("?", sod.Option("z"), sod.Option("a"), sod.Option("m"))
	a := sodtest.ChoiceFor(q, 0.2, 0.5, 0.3)
	if a.Choice != "a" || a.Probabilities["z"] != 0.2 || a.Probabilities["m"] != 0.3 {
		t.Fatalf("ChoiceFor %+v", a)
	}
	tie := sodtest.ChoiceAnswer(map[string]float64{"b": 0.5, "a": 0.5})
	if tie.Choice != "a" || tie.Confidence != 0 {
		t.Fatalf("tie %+v", tie)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("ChoiceFor with wrong count did not panic")
			}
		}()
		sodtest.ChoiceFor(q, 1)
	}()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("ScoreAnswer with wrong count did not panic")
			}
		}()
		sodtest.ScoreAnswer([]any{"a"}, 0.5, 0.5)
	}()
	for probs, want := range map[[3]float64]float64{{1, 0, 0}: 1, {1.0 / 3, 1.0 / 3, 1.0 / 3}: 0, {0.6, 0.2, 0.2}: 0.4} {
		if got := sodtest.Confidence(probs[:]); got-want > 1e-12 || want-got > 1e-12 {
			t.Errorf("Confidence(%v) = %v, want %v", probs, got, want)
		}
	}
	if sodtest.Confidence([]float64{0.3}) != 1 {
		t.Error("Confidence n=1")
	}

	// Fixtures pass the client's validation.
	srv := sodtest.NewServer(t)
	req := sod.NewRequest("s")
	req.Questions["c"] = q
	req.Questions["s"] = sod.Score("?", "lo", "mid", "hi")
	req.Questions["n"] = sod.Noul("?")
	srv.Answer("c", a)
	srv.Answer("s", sodtest.ScoreAnswer([]any{"lo", "mid", "hi"}, 0.1, 0.3, 0.6))
	srv.Answer("n", sodtest.NoulAnswer(1))
	if _, err := srv.Client(t).SystemOne(t.Context(), req); err != nil {
		t.Fatal(err)
	}
}

func TestReadFile(t *testing.T) {
	for _, name := range []string{"api_score_request.json", "api_score_response.json", "api_noul_response.json",
		"api_choice_response.json", "error_422.json", "error_auth.json"} {
		if b := sodtest.ReadFile(t, name); !json.Valid(b) {
			t.Errorf("%s is not valid JSON", name)
		}
	}
}

func TestHandlerInBubble(t *testing.T) {
	fake := sodtest.NewServer(t)
	synctest.Test(t, func(t *testing.T) {
		fake.SetLatency(time.Hour)
		defer fake.SetLatency(0)
		ts := httptest.NewTestServer(t, fake.Handler())
		hc := ts.Client()
		c, err := fake.NewClientFor(ts.URL, hc, sod.WithAttemptTimeout(0))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err := c.SystemOne(t.Context(), request()); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d != time.Hour {
			t.Fatalf("latency %v", d)
		}
	})
}
