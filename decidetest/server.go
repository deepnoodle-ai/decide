// Package decidetest provides a fake TypeSafe API server and answer
// fixtures, so code built on package decide can be tested without an API
// key or a network.
//
// The server speaks the native HTTP API (POST /v1/systemone and GET
// /v1/models), checks the bearer key, records every request, and answers
// with deterministic defaults, canned answers, or a custom Responder. Faults
// such as 429 with Retry-After or 529 can be queued to exercise retries.
//
// Default answers and fixtures are stand-ins. They are shaped like real
// answers and pass the client's validation, but they say nothing about what
// a real model would answer. A question with no canned answer gets: Noul
// 0.5; Choice a uniform distribution with the first option chosen; Score a
// uniform distribution with score (n-1)/2 and the legend taken from the
// question. Unknown question types get a RawAnswer carrying only the type.
//
// The package imports testing only for the testing.TB helpers (NewServer,
// Client, ReadFile). Importing it from a main package, for example to run
// an example without an API key, is fine: it never calls testing.Init, so
// it registers no test flags. Use Start and NewClient there.
package decidetest

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
)

// Server is a fake TypeSafe API. It is safe for concurrent use.
type Server struct {
	URL string // base URL, for decide.WithBaseURL

	ts *httptest.Server

	// Set by options before the server starts; read-only afterwards.
	apiKey   string
	resolved string
	models   []decide.Model

	mu        sync.Mutex
	responder Responder // nil: default responder
	canned    map[string]decide.Answer
	faults    []*fault
	latency   time.Duration
	records   []Recorded
	counter   int
}

// Recorded is one request the server received.
type Recorded struct {
	Method, Path  string
	Authorization string // raw header value; test keys only
	Header        http.Header
	Body          []byte
	Request       *decide.Request // decoded POST body; nil for GET or on decode failure
	RequestID     string          // the id the server sent back
}

// Responder produces the answer to a decoded POST /v1/systemone request. A
// returned *decide.APIError is written with its StatusCode and Body.
type Responder func(req *decide.Request) (*decide.Response, error)

// defaultAPIKey is the bearer key the server expects unless WithAPIKey is
// given. It is at least 8 characters, so the client's key redaction applies
// in tests.
const defaultAPIKey = "test-key-00000000"

func newServer(opts []Option) *Server {
	s := &Server{
		apiKey:   defaultAPIKey,
		resolved: "jev-1.13.0",
		models: []decide.Model{
			{Name: "jev-latest", Description: "Most recent stable Jev release.", ReleaseDate: "2026-09-01"},
			{Name: "jev-preview", Description: "Most recent Jev release of any kind.", ReleaseDate: "2026-09-01"},
		},
		canned: make(map[string]decide.Answer),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start starts the fake server on a loopback listener and does not need a
// testing.TB. A listen failure is returned as an error. The caller must call
// Close.
func Start(opts ...Option) (*Server, error) {
	s := newServer(opts)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("decidetest: listen: %w", err)
	}
	// Built directly rather than with httptest.NewServer, which panics when
	// it cannot listen; here a listen failure is Start's error.
	s.ts = &httptest.Server{Listener: l, Config: &http.Server{Handler: s.Handler()}}
	s.ts.Start()
	s.URL = s.ts.URL
	return s, nil
}

// NewServer calls Start, fails the test on error, and registers Close with
// tb.Cleanup.
func NewServer(tb testing.TB, opts ...Option) *Server {
	tb.Helper()
	s, err := Start(opts...)
	if err != nil {
		tb.Fatalf("%v", err)
	}
	tb.Cleanup(s.Close)
	return s
}

// Close shuts the server down. It is safe to call more than once.
func (s *Server) Close() {
	if s.ts != nil {
		s.ts.Close()
	}
}

// Handler returns the server's handler, so a test can mount it on
// httptest.NewTestServer inside a synctest bubble. State (faults, records,
// canned answers) is shared with s.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

// NewClient returns a client pointed at the server. It applies, in order,
// WithoutEnvironment, WithBaseURL(s.URL), WithAPIKey(expected key), and
// WithRetryBackoff(time.Millisecond, 5*time.Millisecond), then opts.
func (s *Server) NewClient(opts ...decide.ClientOption) (*decide.Client, error) {
	return s.NewClientFor(s.URL, nil, opts...)
}

// NewClientFor is NewClient for a server mounted elsewhere, typically
// Handler() on httptest.NewTestServer inside a synctest bubble: pass that
// server's URL and Client(). hc may be nil for the default HTTP client.
func (s *Server) NewClientFor(baseURL string, hc *http.Client, opts ...decide.ClientOption) (*decide.Client, error) {
	all := []decide.ClientOption{
		decide.WithoutEnvironment(),
		decide.WithBaseURL(baseURL),
		decide.WithAPIKey(s.apiKey),
		decide.WithRetryBackoff(time.Millisecond, 5*time.Millisecond),
	}
	if hc != nil {
		all = append(all, decide.WithHTTPClient(hc))
	}
	return decide.NewClient(append(all, opts...)...)
}

// Client is NewClient that fails the test on error.
func (s *Server) Client(tb testing.TB, opts ...decide.ClientOption) *decide.Client {
	tb.Helper()
	c, err := s.NewClient(opts...)
	if err != nil {
		tb.Fatalf("decidetest: NewClient: %v", err)
	}
	return c
}

// Respond replaces the responder. Canned answers set with Answer are used
// only by the default responder.
func (s *Server) Respond(r Responder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responder = r
}

// Answer sets a canned answer for a question key, used by the default
// responder in place of its deterministic default.
func (s *Server) Answer(key string, a decide.Answer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.canned[key] = a
}

// FailNext queues a failure response with the given status. Faults are
// consumed in order, one per request, before the auth check.
func (s *Server) FailNext(status int, opts ...FaultOption) {
	f := &fault{status: status, headers: map[string]string{}}
	for _, o := range opts {
		o(f)
	}
	if f.body == nil {
		f.body = defaultFaultBody(status)
	}
	s.queue(f)
}

// FailNextConn makes the next request hijack and close the connection
// without writing a response.
func (s *Server) FailNextConn() { s.queue(&fault{closeConn: true}) }

func (s *Server) queue(f *fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = append(s.faults, f)
}

// Overloaded queues n 529 responses.
func (s *Server) Overloaded(n int) {
	for range n {
		s.FailNext(529)
	}
}

// SetLatency sets a delay before each response.
func (s *Server) SetLatency(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latency = d
}

// Requests returns a copy of the recorded requests, in arrival order.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recorded(nil), s.records...)
}

// Reset clears records, faults, canned answers, and latency, and restores
// the default responder.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records, s.faults, s.responder, s.latency = nil, nil, nil, 0
	s.canned = make(map[string]decide.Answer)
}

const requestIDHeader = "x-typesafe-request-id"

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec := Recorded{
		Method:        r.Method,
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		Header:        r.Header.Clone(),
		Body:          body,
	}
	var decoded *decide.Request
	var decodeErr error
	if r.Method == http.MethodPost {
		decoded = &decide.Request{}
		if decodeErr = decoded.UnmarshalJSON(body); decodeErr != nil {
			decoded = nil
		}
		rec.Request = decoded // nil on decode failure
	}

	s.mu.Lock()
	s.counter++
	rec.RequestID = "req_" + strconv.Itoa(s.counter)
	s.records = append(s.records, rec)
	latency := s.latency
	// Faults pop at arrival, before latency is applied, so concurrent
	// requests consume them in arrival order; popping after the delay would
	// let whichever request wakes first take the fault.
	var f *fault
	if len(s.faults) > 0 {
		f, s.faults = s.faults[0], s.faults[1:]
	}
	responder := s.responder
	s.mu.Unlock()

	w.Header().Set(requestIDHeader, rec.RequestID)
	if !wait(r.Context(), latency) {
		return
	}
	if f != nil {
		if f.closeConn {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
					return
				}
			}
			panic(http.ErrAbortHandler) // closes the connection without a response
		}
		if !wait(r.Context(), f.delay) {
			return
		}
		for k, v := range f.headers {
			w.Header().Set(k, v)
		}
		writeJSON(w, f.status, f.body)
		return
	}

	// Discrepancy: /api says 401 for a missing or invalid key, but a live
	// request without a key returned 403. The fake matches the live behavior.
	switch auth := r.Header.Get("Authorization"); {
	case auth == "":
		writeJSON(w, http.StatusForbidden, authBody("Not authenticated"))
		return
	case auth != "Bearer "+s.apiKey:
		writeJSON(w, http.StatusUnauthorized, authBody("Invalid API key"))
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
		b, err := (&decide.ModelList{Models: s.models}).MarshalJSON()
		if err != nil {
			b, _ = json.Marshal(map[string]string{"detail": err.Error()})
			writeJSON(w, http.StatusInternalServerError, b)
			return
		}
		writeJSON(w, http.StatusOK, b)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/systemone":
		s.systemOne(w, decoded, decodeErr, len(body), responder)
	default:
		writeJSON(w, http.StatusNotFound, []byte(`{"detail":"Not Found"}`))
	}
}

func (s *Server) systemOne(w http.ResponseWriter, req *decide.Request, decodeErr error, bodyLen int, responder Responder) {
	if decodeErr != nil {
		writeJSON(w, http.StatusUnprocessableEntity, validationBody([]any{"body"}, "json_invalid", decodeErr.Error()))
		return
	}
	if len(req.Questions) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, validationBody([]any{"body", "questions"}, "too_short",
			"Dictionary should have at least 1 item after validation, not 0"))
		return
	}
	var resp *decide.Response
	var err error
	if responder != nil {
		resp, err = responder(req)
	} else {
		resp = s.defaultResponse(req, bodyLen)
	}
	if err != nil {
		if ae, ok := errors.AsType[*decide.APIError](err); ok {
			b := ae.Body
			if len(b) == 0 {
				b = defaultFaultBody(ae.StatusCode)
			}
			if ae.RetryAfter > 0 {
				w.Header().Set("retry-after-ms", strconv.FormatInt(ae.RetryAfter.Milliseconds(), 10))
			}
			writeJSON(w, ae.StatusCode, b)
			return
		}
		b, _ := json.Marshal(map[string]string{"detail": err.Error()})
		writeJSON(w, http.StatusInternalServerError, b)
		return
	}
	if resp == nil {
		resp = &decide.Response{}
	}
	resp.Model = cmp.Or(resp.Model, s.resolved)
	b, err := resp.MarshalJSON()
	if err != nil {
		b, _ = json.Marshal(map[string]string{"detail": "decidetest: encode response: " + err.Error()})
		writeJSON(w, http.StatusInternalServerError, b)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// defaultResponse answers every question with a canned answer if set, else
// a deterministic default: Noul 0.5; Choice uniform with the first option
// chosen; Score uniform with score (n-1)/2. Unknown question types get a
// *decide.RawAnswer carrying only "type".
//
// Usage is a stand-in: input tokens are the body length divided by 4 and
// output tokens are 2 per question. It is not how TypeSafe counts tokens.
func (s *Server) defaultResponse(req *decide.Request, bodyLen int) *decide.Response {
	s.mu.Lock()
	canned := make(map[string]decide.Answer, len(s.canned))
	maps.Copy(canned, s.canned)
	s.mu.Unlock()

	resp := &decide.Response{
		Model:   s.resolved,
		Answers: make(map[string]decide.Answer, len(req.Questions)),
		Usage:   decide.Usage{InputTokens: bodyLen / 4, OutputTokens: 2 * len(req.Questions)},
	}
	for key, q := range req.Questions {
		if a, ok := canned[key]; ok {
			resp.Answers[key] = a
			continue
		}
		resp.Answers[key] = defaultAnswer(q)
	}
	return resp
}

func defaultAnswer(q decide.Question) decide.Answer {
	switch q := q.(type) {
	case *decide.NoulQuestion:
		return NoulAnswer(0.5)
	case *decide.ChoiceQuestion:
		n := len(q.Criteria)
		a := &decide.ChoiceAnswer{Probabilities: make(map[string]float64, n)}
		ps := make([]float64, n)
		for i, o := range q.Criteria {
			a.Probabilities[o.Key] = 1 / float64(n)
			ps[i] = 1 / float64(n)
		}
		if n > 0 {
			a.Choice = q.Criteria[0].Key
		}
		a.Confidence = Confidence(ps)
		return a
	case *decide.ScoreQuestion:
		n := len(q.Criteria)
		ps := make([]float64, n)
		for i := range ps {
			ps[i] = 1 / float64(n)
		}
		a := ScoreAnswer(q.Criteria, ps...)
		a.Score = float64(n-1) / 2 // exact, rather than a float sum
		return a
	}
	b, _ := json.Marshal(map[string]string{"type": q.QuestionType()})
	return &decide.RawAnswer{Type: q.QuestionType(), JSON: b}
}

// wait sleeps for d, reporting false if ctx ends first.
func wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func authBody(msg string) []byte {
	b, _ := json.Marshal(map[string]any{"detail": map[string]string{
		"error_type": "authentication_error",
		"message":    msg,
	}})
	return b
}

func validationBody(loc []any, typ, msg string) []byte {
	b, _ := json.Marshal(map[string]any{"detail": []map[string]any{{
		"loc": loc, "msg": msg, "type": typ,
	}}})
	return b
}

func defaultFaultBody(status int) []byte {
	var errType, msg string
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return authBody("Invalid API key")
	case status == http.StatusUnprocessableEntity:
		return validationBody([]any{"body"}, "value_error", "Invalid request")
	case status == http.StatusTooManyRequests:
		errType, msg = "rate_limit_error", "Rate limit exceeded"
	case status == 529:
		errType, msg = "overloaded_error", "Overloaded"
	default:
		b, _ := json.Marshal(map[string]string{"detail": fmt.Sprintf("%d %s", status, http.StatusText(status))})
		return b
	}
	b, _ := json.Marshal(map[string]any{"detail": map[string]string{"error_type": errType, "message": msg}})
	return b
}
