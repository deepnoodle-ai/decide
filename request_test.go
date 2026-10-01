package sod_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/sodtest"
)

func TestVerbatimRequestEncoding(t *testing.T) {
	req := sod.NewRequest("Help! My payouts have been failing for 3 days.",
		sod.WithRequestModel("jev-latest"))
	sod.Ask(req, "frustration", sod.Score("How frustrated is the customer?",
		"Calm", "Frustrated", "Very angry"))
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := compact(t, sodtest.ReadFile(t, "api_score_request.json")); string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	in := `{"state":{"b":1,"a":2},"model":"jev-preview","questions":{"q":{"type":"noul","instructions":"x"}},"trace":"t"}`
	var req sod.Request
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
	var s sod.Request
	if err := json.Unmarshal([]byte(`{"state":"text","questions":{}}`), &s); err != nil || s.State != "text" {
		t.Fatalf("string state: %#v %v", s.State, err)
	}
}

func valid() *sod.Request {
	req := sod.NewRequest("state")
	req.Questions["n"] = sod.Noul("q")
	return req
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		mod  func(r *sod.Request)
	}{
		{"nil state", func(r *sod.Request) { r.State = nil }},
		{"typed nil map state", func(r *sod.Request) { r.State = map[string]any(nil) }},
		{"typed nil pointer state", func(r *sod.Request) { r.State = (*struct{ A int })(nil) }},
		{"null raw state", func(r *sod.Request) { r.State = json.RawMessage("null") }},
		{"bytes state", func(r *sod.Request) { r.State = []byte("x") }},
		{"bytes in map state", func(r *sod.Request) { r.State = map[string]any{"a": []byte("x")} }},
		{"bytes in slice state", func(r *sod.Request) { r.State = []any{"a", []byte("x")} }},
		{"unencodable state", func(r *sod.Request) { r.State = make(chan int) }},
		{"no questions", func(r *sod.Request) { r.Questions = nil }},
		{"empty key", func(r *sod.Request) { r.Questions[""] = sod.Noul("q") }},
		{"nil question", func(r *sod.Request) { r.Questions["x"] = nil }},
		{"typed nil noul", func(r *sod.Request) { r.Questions["x"] = (*sod.NoulQuestion)(nil) }},
		{"typed nil choice", func(r *sod.Request) { r.Questions["x"] = (*sod.ChoiceQuestion)(nil) }},
		{"typed nil score", func(r *sod.Request) { r.Questions["x"] = (*sod.ScoreQuestion)(nil) }},
		{"typed nil raw", func(r *sod.Request) { r.Questions["x"] = (*sod.RawQuestion)(nil) }},
		{"typed nil third-party", func(r *sod.Request) { r.Questions["x"] = (*panicQuestion)(nil) }},
		{"no options", func(r *sod.Request) { r.Questions["c"] = sod.Choice("q") }},
		{"empty option key", func(r *sod.Request) { r.Questions["c"] = sod.Choice("q", sod.Option("")) }},
		{"duplicate option", func(r *sod.Request) {
			r.Questions["c"] = sod.Choice("q", sod.Option("a"), sod.Option("a"))
		}},
		{"one level", func(r *sod.Request) { r.Questions["s"] = sod.Score("q", "only") }},
		{"extra state", func(r *sod.Request) { r.Extra = map[string]any{"state": 1} }},
		{"extra model", func(r *sod.Request) { r.Extra = map[string]any{"model": 1} }},
		{"extra questions", func(r *sod.Request) { r.Extra = map[string]any{"questions": 1} }},
		{"question extra collision", func(r *sod.Request) {
			r.Questions["n"] = &sod.NoulQuestion{Instructions: "q", Extra: map[string]any{"type": "x"}}
		}},
		{"bytes instructions", func(r *sod.Request) { r.Questions["n"] = sod.Noul([]byte("q")) }},
		{"bytes in map instructions", func(r *sod.Request) {
			r.Questions["n"] = sod.Noul(map[string]any{"ask": []byte("q")})
		}},
		{"bytes noul true", func(r *sod.Request) { r.Questions["n"] = sod.Noul("q", sod.NoulTrue([]byte("y"))) }},
		{"bytes noul false", func(r *sod.Request) { r.Questions["n"] = sod.Noul("q", sod.NoulFalse([]byte("n"))) }},
		{"bytes choice description", func(r *sod.Request) {
			r.Questions["c"] = sod.Choice("q", sod.Option("a", []byte("A")))
		}},
		{"bytes in slice description", func(r *sod.Request) {
			r.Questions["c"] = sod.Choice("q", sod.Option("a", []any{[]byte("A")}))
		}},
		{"bytes score level", func(r *sod.Request) { r.Questions["s"] = sod.Score("q", "lo", []byte("hi")) }},
		{"bytes choice instructions", func(r *sod.Request) {
			r.Questions["c"] = sod.Choice([]byte("q"), sod.Option("a"))
		}},
		{"bytes score instructions", func(r *sod.Request) { r.Questions["s"] = sod.Score([]byte("q"), "lo", "hi") }},
		{"bad raw question", func(r *sod.Request) {
			r.Questions["r"] = &sod.RawQuestion{Type: "x", JSON: json.RawMessage(`{"type":"y"}`)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.mod(r)
			if err := r.Validate(); !errors.Is(err, sod.ErrInvalidRequest) {
				t.Fatalf("Validate() = %v, want ErrInvalidRequest", err)
			}
		})
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	var nilReq *sod.Request
	if err := nilReq.Validate(); !errors.Is(err, sod.ErrInvalidRequest) {
		t.Fatalf("nil request: %v", err)
	}
}

func TestValidateAllowsNesting(t *testing.T) {
	r := valid()
	// Deeper nesting is not inspected; one level is.
	r.State = map[string]any{"a": map[string]any{"b": "fine"}}
	r.Questions["s"] = sod.Score("q", nil, map[string]any{"label": "hi"})
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
	c := stubClient(t, stubTransport{systemOne: func(context.Context, *sod.Request) (*sod.Response, error) {
		called = true
		return nil, errors.New("unreachable")
	}})
	r := valid()
	r.State = make(chan int)
	if _, err := c.SystemOne(t.Context(), r); !errors.Is(err, sod.ErrInvalidRequest) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.SystemOne(t.Context(), nil); !errors.Is(err, sod.ErrInvalidRequest) {
		t.Fatalf("nil: err = %v", err)
	}
	for _, o := range []sod.CallOption{sod.WithCallMaxRetries(-1), sod.WithCallAttemptTimeout(-1)} {
		if _, err := c.SystemOne(t.Context(), valid(), o); !errors.Is(err, sod.ErrInvalidRequest) {
			t.Fatalf("bad call option: err = %v", err)
		}
	}
	if called {
		t.Fatal("transport was called")
	}
}

func TestModelDefaulting(t *testing.T) {
	srv := sodtest.NewServer(t)
	c := srv.Client(t, sod.WithModel("jev-preview"))
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
