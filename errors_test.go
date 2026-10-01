package sod_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/sod"
	"github.com/deepnoodle-ai/sod/sodtest"
)

func TestStatusSentinels(t *testing.T) {
	all := []error{sod.ErrAuth, sod.ErrValidation, sod.ErrRateLimited, sod.ErrOverloaded, sod.ErrServer}
	cases := map[int][]error{
		401: {sod.ErrAuth},
		403: {sod.ErrAuth},
		408: nil,
		404: nil,
		422: {sod.ErrValidation},
		429: {sod.ErrRateLimited},
		500: {sod.ErrServer},
		503: {sod.ErrServer},
		529: {sod.ErrOverloaded, sod.ErrServer},
	}
	for status, want := range cases {
		err := error(&sod.APIError{StatusCode: status})
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
	srv := sodtest.NewServer(t)
	srv.FailNext(status, sodtest.FaultBody(body))
	_, err := srv.Client(t, sod.WithMaxRetries(0)).SystemOne(t.Context(), valid())
	return err
}

func TestValidationErrorDetails(t *testing.T) {
	err := failOnce(t, 422, sodtest.ReadFile(t, "error_422.json"))
	ae, ok := errors.AsType[*sod.APIError](err)
	if !ok || !errors.Is(err, sod.ErrValidation) {
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
	if ae.RequestID != "req_1" || !strings.HasSuffix(ae.Error(), "(request_id=req_1)") || !strings.HasPrefix(ae.Error(), "sod: 422 ") {
		t.Fatalf("Error() = %q", ae.Error())
	}
}

func TestAuthErrorBody(t *testing.T) {
	err := failOnce(t, 401, sodtest.ReadFile(t, "error_auth.json"))
	ae, ok := errors.AsType[*sod.APIError](err)
	if !ok || !errors.Is(err, sod.ErrAuth) || ae.Type != "authentication_error" || ae.Message != "Invalid API key" {
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
		if ae, ok := errors.AsType[*sod.APIError](err); !ok || ae.Message != want {
			t.Errorf("%q: message %q, want %q", body, ae.Message, want)
		}
	}
	long := strings.Repeat("x", 300)
	if ae, _ := errors.AsType[*sod.APIError](failOnce(t, 500, []byte(long))); len(ae.Message) != 200 {
		t.Errorf("long body message length %d", len(ae.Message))
	}
}

func TestErrorWithoutRequestID(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer hs.Close()
	c, err := sod.NewClient(sod.WithoutEnvironment(), sod.WithAPIKey("k-12345678"), sod.WithBaseURL(hs.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SystemOne(t.Context(), valid())
	if err == nil || err.Error() != "sod: 400 nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyErrorBody(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer hs.Close()
	c, _ := sod.NewClient(sod.WithoutEnvironment(), sod.WithAPIKey("k-12345678"), sod.WithBaseURL(hs.URL))
	_, err := c.SystemOne(t.Context(), valid())
	if ae, ok := errors.AsType[*sod.APIError](err); !ok || ae.Message != "I'm a teapot" || errors.Unwrap(err) != nil {
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
	c, _ := sod.NewClient(sod.WithoutEnvironment(), sod.WithAPIKey("k-12345678"), sod.WithBaseURL(hs.URL))
	if _, err := c.SystemOne(t.Context(), valid()); !errors.Is(err, sod.ErrDecode) || calls != 1 {
		t.Fatalf("err %v calls %d", err, calls)
	}
	if _, err := c.Models.List(t.Context()); !errors.Is(err, sod.ErrDecode) {
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
		c, _ := sod.NewClient(sod.WithoutEnvironment(), sod.WithAPIKey("k-12345678"), sod.WithBaseURL(hs.URL))
		_, err := c.SystemOne(t.Context(), valid())
		hs.Close()
		if size == limit && err != nil {
			t.Errorf("exactly 32 MiB: %v", err)
		}
		if size > limit && !errors.Is(err, sod.ErrDecode) {
			t.Errorf("over 32 MiB: %v", err)
		}
	}
}

func TestNilModelList(t *testing.T) {
	c := stubClient(t, stubTransport{listModels: func(context.Context) (*sod.ModelList, error) { return nil, nil }})
	if list, err := c.Models.List(t.Context()); list != nil || !errors.Is(err, sod.ErrDecode) {
		t.Fatalf("%v %v", list, err)
	}
}
