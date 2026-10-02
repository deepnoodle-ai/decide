package decide_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func TestStatusSentinels(t *testing.T) {
	all := []error{decide.ErrAuth, decide.ErrValidation, decide.ErrRateLimited, decide.ErrOverloaded, decide.ErrServer}
	cases := map[int][]error{
		401: {decide.ErrAuth},
		403: {decide.ErrAuth},
		408: nil,
		404: nil,
		422: {decide.ErrValidation},
		429: {decide.ErrRateLimited},
		500: {decide.ErrServer},
		503: {decide.ErrServer},
		529: {decide.ErrOverloaded, decide.ErrServer},
	}
	for status, want := range cases {
		err := error(&decide.APIError{StatusCode: status})
		for _, s := range all {
			wantIs := false
			for _, w := range want {
				wantIs = wantIs || w == s
			}
			if errors.Is(err, s) != wantIs {
				t.Errorf("%d: errors.Is(%v) = %v", status, s, !wantIs)
			}
		}
	}
}

// failOnce returns a client that sees one fault with the given body.
func failOnce(t *testing.T, status int, body []byte) error {
	t.Helper()
	srv := decidetest.NewServer(t)
	srv.FailNext(status, decidetest.FaultBody(body))
	_, err := srv.Client(t, decide.WithMaxRetries(0)).SystemOne(t.Context(), valid())
	return err
}

func TestValidationErrorDetails(t *testing.T) {
	err := failOnce(t, 422, decidetest.ReadFile(t, "error_422.json"))
	ae, ok := errors.AsType[*decide.APIError](err)
	if !ok || !errors.Is(err, decide.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
	if len(ae.Details) != 1 || ae.Type != "too_short" {
		t.Fatalf("details %+v type %q", ae.Details, ae.Type)
	}
	d := ae.Details[0]
	if !reflect.DeepEqual(d.Loc, []any{"body", "questions", "frustration", "score", "criteria"}) || d.Ctx["min_length"] != float64(1) {
		t.Fatalf("detail %+v", d)
	}
	if want := "questions.frustration.score.criteria: List should have at least 1 item after validation, not 0"; ae.Message != want {
		t.Fatalf("message %q", ae.Message)
	}
	if ae.RequestID != "req_1" || !strings.HasSuffix(ae.Error(), "(request_id=req_1)") || !strings.HasPrefix(ae.Error(), "decide: 422 ") {
		t.Fatalf("Error() = %q", ae.Error())
	}
}

func TestAuthErrorBody(t *testing.T) {
	err := failOnce(t, 401, decidetest.ReadFile(t, "error_auth.json"))
	ae, ok := errors.AsType[*decide.APIError](err)
	if !ok || !errors.Is(err, decide.ErrAuth) || ae.Type != "authentication_error" || ae.Message != "Invalid API key" {
		t.Fatalf("err = %#v", err)
	}
}

func TestErrorMessageExtraction(t *testing.T) {
	cases := map[string]string{
		`{"detail":"plain detail"}`:                       "plain detail",
		`{"error":"error string"}`:                        "error string",
		`{"error":{"type":"x","message":"error object"}}`: "error object",
		`{"message":"top message"}`:                       "top message",
		`upstream exploded`:                               "upstream exploded",
		`{"detail":[{"loc":["body","questions",0],"msg":"m1","type":"t"},{"loc":[],"msg":"m2","type":"t"}]}`: "questions.0: m1; m2",
	}
	for body, want := range cases {
		err := failOnce(t, 503, []byte(body))
		if ae, ok := errors.AsType[*decide.APIError](err); !ok || ae.Message != want {
			t.Errorf("%q: message %q, want %q", body, ae.Message, want)
		}
	}
	long := strings.Repeat("x", 300)
	if ae, _ := errors.AsType[*decide.APIError](failOnce(t, 500, []byte(long))); len(ae.Message) != 200 {
		t.Errorf("long body message length %d", len(ae.Message))
	}
}

func TestErrorWithoutRequestID(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer hs.Close()
	c, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey("k-12345678"), decide.WithBaseURL(hs.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SystemOne(t.Context(), valid())
	if err == nil || err.Error() != "decide: 400 nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyErrorBody(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer hs.Close()
	c, _ := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey("k-12345678"), decide.WithBaseURL(hs.URL))
	_, err := c.SystemOne(t.Context(), valid())
	if ae, ok := errors.AsType[*decide.APIError](err); !ok || ae.Message != "I'm a teapot" || errors.Unwrap(err) != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeErrorNotRetried(t *testing.T) {
	calls := 0
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`["not an object"]`))
	}))
	defer hs.Close()
	c, _ := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey("k-12345678"), decide.WithBaseURL(hs.URL))
	if _, err := c.SystemOne(t.Context(), valid()); !errors.Is(err, decide.ErrDecode) || calls != 1 {
		t.Fatalf("err %v calls %d", err, calls)
	}
	if _, err := c.Models.List(t.Context()); !errors.Is(err, decide.ErrDecode) {
		t.Fatalf("models: %v", err)
	}
}

func TestBodyCap(t *testing.T) {
	const limit = 32 << 20
	for _, size := range []int{limit, limit + 1} {
		hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := []byte(`{"model":"m","answers":{"n":{"type":"noul","noul":0.5}},"pad":"`)
			body = append(body, bytes.Repeat([]byte("x"), size-len(body)-2)...)
			w.Write(append(body, '"', '}'))
		}))
		c, _ := decide.NewClient(decide.WithoutEnvironment(), decide.WithAPIKey("k-12345678"), decide.WithBaseURL(hs.URL))
		_, err := c.SystemOne(t.Context(), valid())
		hs.Close()
		if size == limit && err != nil {
			t.Errorf("exactly 32 MiB: %v", err)
		}
		if size > limit && !errors.Is(err, decide.ErrDecode) {
			t.Errorf("over 32 MiB: %v", err)
		}
	}
}

func TestNilModelList(t *testing.T) {
	c := stubClient(t, stubTransport{listModels: func(context.Context) (*decide.ModelList, error) { return nil, nil }})
	if list, err := c.Models.List(t.Context()); list != nil || !errors.Is(err, decide.ErrDecode) {
		t.Fatalf("%v %v", list, err)
	}
}
