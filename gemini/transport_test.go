package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/gemini"
)

const key = "AIza-test-gemini-key-00000000"

func newClient(t *testing.T, handler http.HandlerFunc, opts ...decide.ClientOption) *decide.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport, err := gemini.NewTransport(gemini.Config{APIKey: key, BaseURL: srv.URL + "/prefix/v1beta/"})
	if err != nil {
		t.Fatal(err)
	}
	all := []decide.ClientOption{decide.WithoutEnvironment(), decide.WithTransport(transport),
		decide.WithModel("gemini-decision-1"), decide.WithRetryBackoff(0, 0)}
	client, err := decide.NewClient(append(all, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

const answers = `{"answers":{
 "refund":{"type":"noul","noul":0.97,"confidence":0.97},
 "topic":{"type":"choice","choice":"billing","probabilities":{"billing":0.9,"shipping":0.06,"other":0.04},"confidence":0.9},
 "urgency":{"type":"score","score":1.8,"legend":{"0":"low","1":"medium","2":"{\"level\":\"high\"}"},"probabilities":{"0":0.05,"1":0.1,"2":0.85},"confidence":0.85}},
 "usage":{"total_input_tokens":398,"input_tokens_by_modality":[{"modality":"text","tokens":398}],"total_output_tokens":0,"total_tokens":398}}`

func TestTranslatesRequestAndAnswers(t *testing.T) {
	var got map[string]any
	var path, apiKey, auth string
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, apiKey, auth = r.URL.Path, r.Header.Get("x-goog-api-key"), r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("x-goog-request-id", "req_123")
		io.WriteString(w, answers)
	})
	req := decide.NewRequest(map[string]any{"ticket": "Order #123 arrived broken. Refund me."},
		decide.WithRequestExtra("store", false))
	refund := decide.Ask(req, "refund", decide.Noul("Is the customer asking for a refund?",
		decide.NoulTrue("they want money back"), decide.NoulFalse("anything else")))
	topic := decide.Ask(req, "topic", decide.Choice("What is it about?",
		decide.Option("shipping", "delivery"), decide.Option("billing", "payments"), decide.Option("other")))
	urgency := decide.Ask(req, "urgency", decide.Score("How urgent?", "low", "medium", map[string]string{"level": "high"}))
	plain := decide.Ask(req, "plain", decide.Noul("Is it polite?"))

	resp, err := client.SystemOne(context.Background(), req)
	if err == nil {
		t.Fatal("an unanswered question was accepted")
	}
	if path != "/prefix/v1beta/decisions" || apiKey != key || auth != "" || !errors.Is(err, decide.ErrInvalidAnswer) {
		t.Fatalf("path %q key %q auth %q err %v", path, apiKey, auth, err)
	}
	want := `{"input":"{\"ticket\":\"Order #123 arrived broken. Refund me.\"}","model":"gemini-decision-1","questions":{` +
		`"plain":{"instructions":"Is it polite?","type":"noul"},` +
		`"refund":{"criteria":{"false_description":"anything else","true_description":"they want money back"},"instructions":"Is the customer asking for a refund?","type":"noul"},` +
		`"topic":{"criteria":{"billing":"payments","other":"","shipping":"delivery"},"instructions":"What is it about?","type":"choice"},` +
		`"urgency":{"criteria":["low","medium","{\"level\":\"high\"}"],"instructions":"How urgent?","type":"score"}},"store":false}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Fatalf("request body\n got %s\nwant %s", b, want)
	}
	if _, err := plain.From(resp); err == nil {
		t.Fatal("missing answer accepted")
	}

	r, err := refund.From(resp)
	if err != nil || r.Noul != 0.97 {
		t.Fatalf("refund %+v %v", r, err)
	}
	c, err := topic.From(resp)
	if err != nil || c.Choice != "billing" || c.Probabilities["shipping"] != 0.06 || c.Confidence != 0.9 {
		t.Fatalf("topic %+v %v", c, err)
	}
	s, err := urgency.From(resp)
	if err != nil || s.Score != 1.8 || s.Probabilities["2"] != 0.85 || s.Legend["0"] != "low" {
		t.Fatalf("urgency %+v %v", s, err)
	}
	if level, _ := s.Legend["2"].(map[string]any); level["level"] != "high" {
		t.Fatalf("legend keeps the level as asked: %#v", s.Legend["2"])
	}
	if resp.Model != "gemini-decision-1" || resp.RequestID != "req_123" || resp.Usage.InputTokens != 398 {
		t.Fatalf("response %+v", resp)
	}
	if !strings.Contains(string(resp.Raw), `"total_input_tokens":398`) {
		t.Fatalf("Raw is the provider body: %s", resp.Raw)
	}
}

func TestErrorsBecomeAPIErrors(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":{"code":429,"message":"Slow down","status":"RESOURCE_EXHAUSTED"}}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"code":"safety","message":"Blocked %s","details":[{"reason":"OTHER"}]}}`, key)
	}, decide.WithMaxRetries(1))
	req := decide.NewRequest("hello")
	decide.Ask(req, "a", decide.Noul("A?"))
	_, err := client.SystemOne(context.Background(), req)
	var apiErr *decide.APIError
	if !errors.As(err, &apiErr) || errors.Is(err, decide.ErrAuth) {
		t.Fatalf("not a plain APIError: %v", err)
	}
	if calls.Load() != 2 || apiErr.StatusCode != 400 || apiErr.Type != "safety" {
		t.Fatalf("calls %d error %+v", calls.Load(), apiErr)
	}
	if strings.Contains(err.Error(), key) || strings.Contains(string(apiErr.Body), key) {
		t.Fatalf("key leaked: %v %s", err, apiErr.Body)
	}
}

func TestInvalidKeyIsAuthError(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":400,"message":"API key not valid.","status":"INVALID_ARGUMENT",
			"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`)
	})
	req := decide.NewRequest("hello")
	decide.Ask(req, "a", decide.Noul("A?"))
	_, err := client.SystemOne(context.Background(), req)
	var apiErr *decide.APIError
	if !errors.Is(err, decide.ErrAuth) || !errors.As(err, &apiErr) || apiErr.Type != "API_KEY_INVALID" {
		t.Fatalf("got %v", err)
	}
}

func TestRejectsRequestsTheAPICannotTake(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	many := decide.NewRequest("hello")
	for i := range 1001 {
		decide.Ask(many, fmt.Sprint("q", i), decide.Noul("A?"))
	}
	oneOption := decide.NewRequest("hello")
	decide.Ask(oneOption, "c", decide.Choice("Which?", decide.Option("only")))
	badKey := decide.NewRequest("hello")
	decide.Ask(badKey, "has space", decide.Noul("A?"))
	emptyLevel := decide.NewRequest("hello")
	decide.Ask(emptyLevel, "s", decide.Score("How?", "low", " "))
	image := decide.NewRequest("hello", decide.WithRequestExtra("images", []string{"data:image/png;base64,AA=="}))
	decide.Ask(image, "a", decide.Noul("A?"))
	for name, tc := range map[string]struct {
		req  *decide.Request
		want error
	}{
		"1001 questions": {many, decide.ErrInvalidRequest},
		"one option":     {oneOption, decide.ErrInvalidRequest},
		"key":            {badKey, decide.ErrInvalidRequest},
		"empty level":    {emptyLevel, decide.ErrInvalidRequest},
		"images":         {image, errors.ErrUnsupported},
	} {
		if _, err := client.SystemOne(context.Background(), tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("%d invalid requests were sent", calls.Load())
	}
}

func TestMalformedResponses(t *testing.T) {
	for _, body := range []string{
		`not json`,
		`{"answers":null}`,
		`{"usage":{}}`,
		`{"answers":[{"type":"noul","noul":1}]}`,
	} {
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		req := decide.NewRequest("hello")
		decide.Ask(req, "a", decide.Noul("A?"))
		if _, err := client.SystemOne(context.Background(), req); !errors.Is(err, decide.ErrDecode) {
			t.Errorf("%s: got %v, want ErrDecode", body, err)
		}
	}
}

func TestConfig(t *testing.T) {
	if _, err := gemini.NewTransport(gemini.Config{}); !errors.Is(err, decide.ErrNoAPIKey) {
		t.Fatalf("missing key: %v", err)
	}
	for _, base := range []string{"generativelanguage.googleapis.com", "ftp://example.com", "https://user:pw@example.com", "https://example.com?x=1"} {
		if _, err := gemini.NewTransport(gemini.Config{APIKey: key, BaseURL: base}); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("base %q accepted: %v", base, err)
		}
	}
	cfg := gemini.Config{APIKey: key}
	tr, err := gemini.NewTransport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{cfg.String(), fmt.Sprintf("%#v", cfg), tr.String(), fmt.Sprintf("%v", tr)} {
		if strings.Contains(s, key) {
			t.Fatalf("key in %q", s)
		}
	}
	if _, err := tr.ListModels(context.Background()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("ListModels: %v", err)
	}
}
