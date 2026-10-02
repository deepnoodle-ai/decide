package decide_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func TestVerbatimRequestEncoding(t *testing.T) {
	req := decide.NewRequest("Help! My payouts have been failing for 3 days.",
		decide.WithRequestModel("jev-latest"))
	decide.Ask(req, "frustration", decide.Score("How frustrated is the customer?",
		"Calm", "Frustrated", "Very angry"))
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := compact(t, decidetest.ReadFile(t, "api_score_request.json")); string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	in := `{"state":{"b":1,"a":2},"model":"jev-preview","questions":{"q":{"type":"noul","instructions":"x"}},"trace":"t"}`
	var req decide.Request
	if err := json.Unmarshal([]byte(in), &req); err != nil {
		t.Fatal(err)
	}
	if _, ok := req.State.(json.RawMessage); !ok {
		t.Fatalf("object state kept as %T", req.State)
	}
	out, err := json.Marshal(&req)
	if err != nil || string(out) != in {
		t.Fatalf("round trip\n got %s\nwant %s (%v)", out, in, err)
	}
	var s decide.Request
	if err := json.Unmarshal([]byte(`{"state":"text","questions":{}}`), &s); err != nil || s.State != "text" {
		t.Fatalf("string state: %#v %v", s.State, err)
	}
}

func valid() *decide.Request {
	req := decide.NewRequest("state")
	req.Questions["n"] = decide.Noul("q")
	return req
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		mod  func(r *decide.Request)
	}{
		{"nil state", func(r *decide.Request) { r.State = nil }},
		{"typed nil map state", func(r *decide.Request) { r.State = map[string]any(nil) }},
		{"typed nil pointer state", func(r *decide.Request) { r.State = (*struct{ A int })(nil) }},
		{"null raw state", func(r *decide.Request) { r.State = json.RawMessage("null") }},
		{"bytes state", func(r *decide.Request) { r.State = []byte("x") }},
		{"bytes in map state", func(r *decide.Request) { r.State = map[string]any{"a": []byte("x")} }},
		{"bytes in slice state", func(r *decide.Request) { r.State = []any{"a", []byte("x")} }},
		{"unencodable state", func(r *decide.Request) { r.State = make(chan int) }},
		{"no questions", func(r *decide.Request) { r.Questions = nil }},
		{"empty key", func(r *decide.Request) { r.Questions[""] = decide.Noul("q") }},
		{"nil question", func(r *decide.Request) { r.Questions["x"] = nil }},
		{"typed nil noul", func(r *decide.Request) { r.Questions["x"] = (*decide.NoulQuestion)(nil) }},
		{"typed nil choice", func(r *decide.Request) { r.Questions["x"] = (*decide.ChoiceQuestion)(nil) }},
		{"typed nil score", func(r *decide.Request) { r.Questions["x"] = (*decide.ScoreQuestion)(nil) }},
		{"typed nil raw", func(r *decide.Request) { r.Questions["x"] = (*decide.RawQuestion)(nil) }},
		{"typed nil third-party", func(r *decide.Request) { r.Questions["x"] = (*panicQuestion)(nil) }},
		{"no options", func(r *decide.Request) { r.Questions["c"] = decide.Choice("q") }},
		{"empty option key", func(r *decide.Request) { r.Questions["c"] = decide.Choice("q", decide.Option("")) }},
		{"duplicate option", func(r *decide.Request) {
			r.Questions["c"] = decide.Choice("q", decide.Option("a"), decide.Option("a"))
		}},
		{"one level", func(r *decide.Request) { r.Questions["s"] = decide.Score("q", "only") }},
		{"extra state", func(r *decide.Request) { r.Extra = map[string]any{"state": 1} }},
		{"extra model", func(r *decide.Request) { r.Extra = map[string]any{"model": 1} }},
		{"extra questions", func(r *decide.Request) { r.Extra = map[string]any{"questions": 1} }},
		{"question extra collision", func(r *decide.Request) {
			r.Questions["n"] = &decide.NoulQuestion{Instructions: "q", Extra: map[string]any{"type": "x"}}
		}},
		{"bytes instructions", func(r *decide.Request) { r.Questions["n"] = decide.Noul([]byte("q")) }},
		{"bytes in map instructions", func(r *decide.Request) {
			r.Questions["n"] = decide.Noul(map[string]any{"ask": []byte("q")})
		}},
		{"bytes noul true", func(r *decide.Request) { r.Questions["n"] = decide.Noul("q", decide.NoulTrue([]byte("y"))) }},
		{"bytes noul false", func(r *decide.Request) { r.Questions["n"] = decide.Noul("q", decide.NoulFalse([]byte("n"))) }},
		{"bytes choice description", func(r *decide.Request) {
			r.Questions["c"] = decide.Choice("q", decide.Option("a", []byte("A")))
		}},
		{"bytes in slice description", func(r *decide.Request) {
			r.Questions["c"] = decide.Choice("q", decide.Option("a", []any{[]byte("A")}))
		}},
		{"bytes score level", func(r *decide.Request) { r.Questions["s"] = decide.Score("q", "lo", []byte("hi")) }},
		{"bytes choice instructions", func(r *decide.Request) {
			r.Questions["c"] = decide.Choice([]byte("q"), decide.Option("a"))
		}},
		{"bytes score instructions", func(r *decide.Request) { r.Questions["s"] = decide.Score([]byte("q"), "lo", "hi") }},
		{"bad raw question", func(r *decide.Request) {
			r.Questions["r"] = &decide.RawQuestion{Type: "x", JSON: json.RawMessage(`{"type":"y"}`)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.mod(r)
			if err := r.Validate(); !errors.Is(err, decide.ErrInvalidRequest) {
				t.Fatalf("Validate() = %v, want ErrInvalidRequest", err)
			}
		})
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	var nilReq *decide.Request
	if err := nilReq.Validate(); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("nil request: %v", err)
	}
}

func TestValidateAllowsNesting(t *testing.T) {
	r := valid()
	// Deeper nesting is not inspected; one level is.
	r.State = map[string]any{"a": map[string]any{"b": "fine"}}
	r.Questions["s"] = decide.Score("q", nil, map[string]any{"label": "hi"})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRawMessageStatePassesThrough(t *testing.T) {
	r := valid()
	r.State = json.RawMessage(`{"ticket":{"id":7,"text":"hi"}}`)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), `{"state":{"ticket":{"id":7,"text":"hi"}},`) {
		t.Fatalf("got %s", b)
	}
}

func TestInvalidRequestMakesNoCall(t *testing.T) {
	called := false
	c := stubClient(t, stubTransport{systemOne: func(context.Context, *decide.Request) (*decide.Response, error) {
		called = true
		return nil, errors.New("unreachable")
	}})
	r := valid()
	r.State = make(chan int)
	if _, err := c.SystemOne(t.Context(), r); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.SystemOne(t.Context(), nil); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("nil: err = %v", err)
	}
	for _, o := range []decide.CallOption{decide.WithCallMaxRetries(-1), decide.WithCallAttemptTimeout(-1)} {
		if _, err := c.SystemOne(t.Context(), valid(), o); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Fatalf("bad call option: err = %v", err)
		}
	}
	if called {
		t.Fatal("transport was called")
	}
}

func TestModelDefaulting(t *testing.T) {
	srv := decidetest.NewServer(t)
	c := srv.Client(t, decide.WithModel("jev-preview"))
	req := valid()
	before := *req
	if _, err := c.SystemOne(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*req, before) || req.Model != "" {
		t.Fatalf("caller's request modified: %+v", req)
	}
	if got := srv.Requests()[0].Request.Model; got != "jev-preview" {
		t.Fatalf("sent model %q", got)
	}
	req.Model = "jev-1.13.0"
	if _, err := c.SystemOne(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if got := srv.Requests()[1].Request.Model; got != "jev-1.13.0" {
		t.Fatalf("request model not honored: %q", got)
	}
}

// panicQuestion dereferences its receiver, as many third-party
// implementations will; a typed nil must not crash Validate.
type panicQuestion struct{ text string }

func (q *panicQuestion) QuestionType() string { return "tstest-panic" }
func (q *panicQuestion) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"type": "tstest-panic", "instructions": q.text})
}
