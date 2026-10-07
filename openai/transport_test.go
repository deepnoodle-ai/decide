package openai_test

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
	"github.com/deepnoodle-ai/decide/openai"
)

const key = "sk-test-openai-key-00000000"

func newClient(t *testing.T, handler http.HandlerFunc, opts ...decide.ClientOption) *decide.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport, err := openai.NewTransport(openai.Config{APIKey: key, BaseURL: srv.URL + "/prefix/v1/"})
	if err != nil {
		t.Fatal(err)
	}
	all := []decide.ClientOption{decide.WithoutEnvironment(), decide.WithTransport(transport),
		decide.WithModel("gpt-6-luna"), decide.WithRetryBackoff(0, 0)}
	client, err := decide.NewClient(append(all, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

const answers = `{"model":"gpt-6-luna","answers":[
 {"type":"predicate","name":"refund","probability":0.97},
 {"type":"choice","name":"topic","choice":"billing","probabilities":[{"value":"billing","probability":0.9},{"value":"shipping","probability":0.06},{"value":"other","probability":0.04}],"confidence":0.85},
 {"type":"score","name":"urgency","score":1.8,"probabilities":[{"value":0,"label":"low","probability":0.05},{"value":1,"label":"medium","probability":0.1},{"value":2,"label":"{\"level\":\"high\"}","probability":0.85}],"confidence":0.8}],
 "usage":{"input_tokens":398,"input_tokens_details":{"cached_tokens":0},"output_tokens":0,"total_tokens":398}}`

func TestTranslatesRequestAndAnswers(t *testing.T) {
	var got map[string]any
	var path, auth string
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("x-request-id", "req_123")
		io.WriteString(w, answers)
	})
	req := decide.NewRequest(map[string]any{"ticket": "Order #123 arrived broken. Refund me."},
		decide.WithRequestExtra("store", false))
	refund := decide.Ask(req, "refund", decide.Noul("Is the customer asking for a refund?",
		decide.NoulTrue("they want money back"), decide.NoulFalse("anything else")))
	topic := decide.Ask(req, "topic", decide.Choice("What is it about?",
		decide.Option("shipping", "delivery"), decide.Option("billing", "payments"), decide.Option("other")))
	urgency := decide.Ask(req, "urgency", decide.Score("How urgent?", "low", "medium", map[string]string{"level": "high"}))

	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/prefix/v1/decisions" || auth != "Bearer "+key {
		t.Fatalf("path %q auth %q", path, auth)
	}
	want := `{"input":"{\"ticket\":\"Order #123 arrived broken. Refund me.\"}","model":"gpt-6-luna","questions":[` +
		`{"instructions":"Is the customer asking for a refund?\n\nYes means: they want money back\n\nNo means: anything else","name":"refund","type":"predicate"},` +
		`{"choices":[{"description":"delivery","value":"shipping"},{"description":"payments","value":"billing"},{"value":"other"}],"instructions":"What is it about?","name":"topic","type":"choice"},` +
		`{"instructions":"How urgent?","levels":[{"label":"low"},{"label":"medium"},{"label":"{\"level\":\"high\"}"}],"name":"urgency","type":"score"}],"store":false}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Fatalf("request body\n got %s\nwant %s", b, want)
	}

	r, err := refund.From(resp)
	if err != nil || r.Noul != 0.97 {
		t.Fatalf("refund %+v %v", r, err)
	}
	c, err := topic.From(resp)
	if err != nil || c.Choice != "billing" || c.Probabilities["shipping"] != 0.06 || c.Confidence != 0.85 {
		t.Fatalf("topic %+v %v", c, err)
	}
	s, err := urgency.From(resp)
	if err != nil || s.Score != 1.8 || s.Probabilities["2"] != 0.85 || s.Legend["0"] != "low" {
		t.Fatalf("urgency %+v %v", s, err)
	}
	if level, _ := s.Legend["2"].(map[string]any); level["level"] != "high" {
		t.Fatalf("legend keeps the level as asked: %#v", s.Legend["2"])
	}
	if resp.Model != "gpt-6-luna" || resp.RequestID != "req_123" || resp.Usage.InputTokens != 398 || resp.Invalid != nil {
		t.Fatalf("response %+v", resp)
	}
	if !strings.Contains(string(resp.Raw), `"answers":[`) {
		t.Fatalf("Raw is the provider body: %s", resp.Raw)
	}
}

func TestRefusalIsInvalidForItsQuestion(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"gpt-6-luna","answers":[{"type":"refusal","name":"rude"}]}`)
	})
	req := decide.NewRequest("hello")
	rude := decide.Ask(req, "rude", decide.Noul("Is it rude?"))
	resp, err := client.SystemOne(context.Background(), req)
	if !errors.Is(err, decide.ErrInvalidAnswer) || !strings.Contains(err.Error(), `"refusal"`) {
		t.Fatalf("refusal accepted: %v", err)
	}
	if _, err := rude.From(resp); err == nil || resp.Invalid["rude"] == nil {
		t.Fatalf("refusal accepted: %+v %v", resp.Invalid, err)
	}
}

func TestErrorsBecomeAPIErrors(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_err")
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":{"message":"Slow down","type":"requests","code":"rate_limit_exceeded"}}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"Bad key %s","type":"invalid_request_error","param":"questions","code":"array_above_max_length"}}`, key)
	}, decide.WithMaxRetries(1))
	req := decide.NewRequest("hello")
	decide.Ask(req, "a", decide.Noul("A?"))
	_, err := client.SystemOne(context.Background(), req)
	var apiErr *decide.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("not an APIError: %v", err)
	}
	if calls.Load() != 2 || apiErr.StatusCode != 400 || apiErr.Type != "array_above_max_length" || apiErr.RequestID != "req_err" {
		t.Fatalf("calls %d error %+v", calls.Load(), apiErr)
	}
	if strings.Contains(err.Error(), key) || strings.Contains(string(apiErr.Body), key) {
		t.Fatalf("key leaked: %v %s", err, apiErr.Body)
	}
}

func TestRejectsRequestsTheAPICannotTake(t *testing.T) {
	var calls atomic.Int32
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	many := decide.NewRequest("hello")
	for i := range 201 {
		decide.Ask(many, fmt.Sprint("q", i), decide.Noul("A?"))
	}
	oneOption := decide.NewRequest("hello")
	decide.Ask(oneOption, "c", decide.Choice("Which?", decide.Option("only")))
	image := decide.NewRequest("hello", decide.WithRequestExtra("images", []string{"data:image/png;base64,AA=="}))
	decide.Ask(image, "a", decide.Noul("A?"))
	for name, tc := range map[string]struct {
		req  *decide.Request
		want error
	}{
		"201 questions": {many, decide.ErrInvalidRequest},
		"one option":    {oneOption, decide.ErrInvalidRequest},
		"images":        {image, errors.ErrUnsupported},
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
		`{"answers":{"a":{"type":"noul","noul":1}}}`,
		`{"answers":[{"type":"predicate","probability":1}]}`,
		`{"answers":[{"type":"predicate","name":"a","probability":1},{"type":"predicate","name":"a","probability":0}]}`,
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
	if _, err := openai.NewTransport(openai.Config{}); !errors.Is(err, decide.ErrNoAPIKey) {
		t.Fatalf("missing key: %v", err)
	}
	for _, base := range []string{"api.openai.com/v1", "ftp://example.com", "https://user:pw@example.com", "https://example.com?x=1"} {
		if _, err := openai.NewTransport(openai.Config{APIKey: key, BaseURL: base}); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Errorf("base %q accepted: %v", base, err)
		}
	}
	cfg := openai.Config{APIKey: key}
	tr, err := openai.NewTransport(cfg)
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
