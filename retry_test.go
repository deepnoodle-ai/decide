package decide_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// All tests here run inside synctest bubbles, so retry sleeps cost no wall
// time. HTTP goes over httptest.NewTestServer's in-memory network; a
// loopback server must not be used inside a bubble.

const retryKey = "retry-test-key-123"

// newFake is created outside the bubble; its loopback listener is unused.
func newFake(t *testing.T) *decidetest.Server {
	return decidetest.NewServer(t, decidetest.WithAPIKey(retryKey))
}

// bubbleClient mounts fake's Handler on an in-memory server inside the
// current bubble and returns a client for it.
func bubbleClient(t *testing.T, fake *decidetest.Server, opts ...decide.ClientOption) *decide.Client {
	t.Helper()
	ts := httptest.NewTestServer(t, fake.Handler())
	hc := ts.Client() // sets ts.URL
	c, err := fake.NewClientFor(ts.URL, hc, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rawBubbleClient is bubbleClient for a hand-written handler.
func rawBubbleClient(t *testing.T, h http.Handler, opts ...decide.ClientOption) *decide.Client {
	t.Helper()
	ts := httptest.NewTestServer(t, h)
	hc := ts.Client()
	c, err := decide.NewClient(append([]decide.ClientOption{
		decide.WithoutEnvironment(), decide.WithAPIKey(retryKey),
		decide.WithBaseURL(ts.URL), decide.WithHTTPClient(hc),
	}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func elapsed(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}

func TestRetryAfterSeconds(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(429, decidetest.RetryAfter(time.Second))
		c := bubbleClient(t, fake)
		var err error
		d := elapsed(func() { _, err = c.SystemOne(t.Context(), valid()) })
		if err != nil || d != time.Second {
			t.Fatalf("err %v, slept %v, want exactly 1s", err, d)
		}
		recs := fake.Requests()
		if len(recs) != 2 || recs[0].Header.Get("X-TypeSafe-Retry-Count") != "" || recs[1].Header.Get("X-TypeSafe-Retry-Count") != "1" {
			t.Fatalf("retry-count headers: %d requests", len(recs))
		}
	})
}

func TestRetryAfterMSWins(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(429, decidetest.RetryAfter(3*time.Second), decidetest.RetryAfterMS(250))
		c := bubbleClient(t, fake)
		if d := elapsed(func() { c.SystemOne(t.Context(), valid()) }); d != 250*time.Millisecond {
			t.Fatalf("slept %v", d)
		}
	})
}

func TestRetryAfterHTTPDate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		fake := newFakeInBubbleHandler(func(w http.ResponseWriter) bool {
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
				w.WriteHeader(429)
				return true
			}
			return false
		})
		c := rawBubbleClient(t, fake)
		var err error
		if d := elapsed(func() { _, err = c.SystemOne(t.Context(), valid()) }); err != nil || d != 2*time.Second {
			t.Fatalf("err %v slept %v", err, d)
		}
	})
}

// newFakeInBubbleHandler answers every request with a valid noul response
// unless intercept writes something first.
func newFakeInBubbleHandler(intercept func(w http.ResponseWriter) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if intercept(w) {
			return
		}
		w.Write([]byte(`{"model":"jev-1.13.0","answers":{"n":{"type":"noul","noul":0.5}}}`))
	})
}

func TestLongRetryAfterFallsBackToBackoff(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(429, decidetest.RetryAfter(2*time.Minute))
		c := bubbleClient(t, fake, decide.WithRetryBackoff(100*time.Millisecond, time.Second))
		if d := elapsed(func() { c.SystemOne(t.Context(), valid()) }); d < 75*time.Millisecond || d > 100*time.Millisecond {
			t.Fatalf("slept %v, want backoff in [75ms, 100ms]", d)
		}
	})
}

func TestBackoffBounds(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.Overloaded(2)
		c := bubbleClient(t, fake, decide.WithRetryBackoff(100*time.Millisecond, 150*time.Millisecond))
		var err error
		d := elapsed(func() { _, err = c.SystemOne(t.Context(), valid()) })
		// Retry 0 waits in [75ms, 100ms]; retry 1 is capped at 150ms, so [112.5ms, 150ms].
		if err != nil || d < 187*time.Millisecond || d > 250*time.Millisecond {
			t.Fatalf("err %v, slept %v", err, d)
		}
	})
}

func TestNotRetried422(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(422)
		c := bubbleClient(t, fake)
		if _, err := c.SystemOne(t.Context(), valid()); !errors.Is(err, decide.ErrValidation) || len(fake.Requests()) != 1 {
			t.Fatalf("err %v, %d requests", err, len(fake.Requests()))
		}
	})
}

func TestRetriesExhausted(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.Overloaded(3)
		logger, logs := debugLogger()
		c := bubbleClient(t, fake, decide.WithLogger(logger))
		_, err := c.SystemOne(t.Context(), valid())
		ae, ok := errors.AsType[*decide.APIError](err)
		if !ok || ae.StatusCode != 529 || ae.RequestID != "req_3" || !errors.Is(err, decide.ErrOverloaded) {
			t.Fatalf("err %v", err)
		}
		out := logs.String()
		for _, want := range []string{
			`level=INFO msg="decide retry" op=systemone attempt=1`,
			`level=INFO msg="decide retry" op=systemone attempt=2`,
			`level=WARN msg="decide retries exhausted" op=systemone attempts=3`,
			`level=DEBUG msg="decide request" op=systemone attempt=2`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("log missing %q:\n%s", want, out)
			}
		}
	})
}

func TestHugeRetryAfterFallsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		h := newFakeInBubbleHandler(func(w http.ResponseWriter) bool {
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "1e300")
				w.WriteHeader(429)
				return true
			}
			return false
		})
		c := rawBubbleClient(t, h, decide.WithRetryBackoff(100*time.Millisecond, time.Second))
		var err error
		if d := elapsed(func() { _, err = c.SystemOne(t.Context(), valid()) }); err != nil || d > 100*time.Millisecond || d < 75*time.Millisecond {
			t.Fatalf("err %v slept %v", err, d)
		}
	})
}

func TestDeadlineShortcutLogsWarn(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(429, decidetest.RetryAfter(10*time.Second))
		logger, logs := debugLogger()
		c := bubbleClient(t, fake, decide.WithLogger(logger))
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		c.SystemOne(ctx, valid())
		out := logs.String()
		if !strings.Contains(out, `level=WARN msg="decide retries exhausted" op=systemone attempts=1`) {
			t.Fatalf("no Warn on deadline shortcut:\n%s", out)
		}
		if n := strings.Count(out, "status="); n != 1 {
			t.Fatalf("status logged %d times:\n%s", n, out)
		}
	})
}

func TestSuccessLogOmitsStatus(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		logger, logs := debugLogger()
		c := bubbleClient(t, fake, decide.WithLogger(logger))
		if _, err := c.SystemOne(t.Context(), valid()); err != nil {
			t.Fatal(err)
		}
		out := logs.String()
		if strings.Contains(out, "status=") || strings.Count(out, "request_id=req_") != 1 || !strings.Contains(out, "resolved_model=jev-1.13.0") {
			t.Fatalf("success record:\n%s", out)
		}
	})
}

// A transport may return a response and an error together.
func TestResponseAndErrorLoggedOnce(t *testing.T) {
	logger, logs := debugLogger()
	tr := stubTransport{systemOne: func(context.Context, *decide.Request) (*decide.Response, error) {
		return &decide.Response{RequestID: "req_both"}, &decide.APIError{StatusCode: 400, RequestID: "req_both"}
	}}
	c := stubClient(t, tr, decide.WithLogger(logger))
	c.SystemOne(t.Context(), valid())
	out := logs.String()
	if strings.Count(out, " status=") != 1 || strings.Count(out, " request_id=") != 1 {
		t.Fatalf("duplicated attributes:\n%s", out)
	}
}

func TestCancelDuringSleep(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(429, decidetest.RetryAfter(10*time.Second))
		c := bubbleClient(t, fake)
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(time.Second, cancel)
		var err error
		d := elapsed(func() { _, err = c.SystemOne(ctx, valid()) })
		ae, ok := errors.AsType[*decide.APIError](err)
		if d != time.Second || !errors.Is(err, context.Canceled) || !ok || ae.StatusCode != 429 {
			t.Fatalf("after %v: %v", d, err)
		}
	})
}

func TestCancelBeforeAnyFailure(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.SetLatency(5 * time.Second)
		defer fake.SetLatency(0)
		c := bubbleClient(t, fake)
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(time.Second, cancel)
		if _, err := c.SystemOne(ctx, valid()); err != context.Canceled {
			t.Fatalf("err = %#v, want bare context.Canceled", err)
		}
	})
}

func TestDeadlineShorterThanDelay(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(429, decidetest.RetryAfter(10*time.Second))
		c := bubbleClient(t, fake)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		var err error
		d := elapsed(func() { _, err = c.SystemOne(ctx, valid()) })
		if d != 0 || !errors.Is(err, decide.ErrRateLimited) || len(fake.Requests()) != 1 {
			t.Fatalf("after %v: %v", d, err)
		}
	})
}

func TestAttemptTimeoutRetried(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNext(500, decidetest.FaultDelay(5*time.Second))
		c := bubbleClient(t, fake, decide.WithAttemptTimeout(time.Second), decide.WithRetryBackoff(time.Millisecond, time.Millisecond))
		var err error
		d := elapsed(func() { _, err = c.SystemOne(t.Context(), valid()) })
		if err != nil || len(fake.Requests()) != 2 || d > 1100*time.Millisecond {
			t.Fatalf("err %v after %v, %d requests", err, d, len(fake.Requests()))
		}
		// A per-call timeout overrides the client's.
		fake.FailNext(500, decidetest.FaultDelay(5*time.Second))
		d = elapsed(func() {
			_, err = c.SystemOne(t.Context(), valid(), decide.WithCallAttemptTimeout(3*time.Second))
		})
		if err != nil || d < 3*time.Second || d > 3100*time.Millisecond {
			t.Fatalf("per-call timeout: err %v after %v", err, d)
		}
	})
}

func TestCallMaxRetriesZero(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.Overloaded(1)
		c := bubbleClient(t, fake)
		if _, err := c.SystemOne(t.Context(), valid(), decide.WithCallMaxRetries(0)); !errors.Is(err, decide.ErrOverloaded) {
			t.Fatalf("err %v", err)
		}
		if n := len(fake.Requests()); n != 1 {
			t.Fatalf("%d requests", n)
		}
	})
}

func TestConnectionDropRetried(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.FailNextConn()
		c := bubbleClient(t, fake)
		if _, err := c.SystemOne(t.Context(), valid()); err != nil || len(fake.Requests()) != 2 {
			t.Fatalf("err %v, %d requests", err, len(fake.Requests()))
		}
	})
}

func TestModelsListRetried(t *testing.T) {
	fake := newFake(t)
	synctest.Test(t, func(t *testing.T) {
		fake.Overloaded(1)
		c := bubbleClient(t, fake)
		list, err := c.Models.List(t.Context())
		if err != nil || len(list.Models) != 2 || len(fake.Requests()) != 2 {
			t.Fatalf("list %+v err %v", list, err)
		}
	})
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestTransportErrorClassification(t *testing.T) {
	post := func(err error) error {
		return &url.Error{Op: "Post", URL: "https://api.typesafe.ai/v1/systemone", Err: err}
	}
	retried := map[string]error{
		"timeout":      post(&net.OpError{Op: "read", Net: "tcp", Err: timeoutError{}}),
		"ECONNRESET":   post(&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}),
		"ECONNREFUSED": post(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}),
		"EPIPE":        post(fmt.Errorf("write: %w", syscall.EPIPE)),
		"dial":         post(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("no route to host")}),
		"EOF":          post(io.EOF),
		"short body":   io.ErrUnexpectedEOF,
		"idle closed":  post(errors.New("http: server closed idle connection")),
	}
	notRetried := map[string]error{
		"unknown authority": post(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
		"hostname":          post(x509.HostnameError{Host: "x"}),
		"invalid cert":      post(x509.CertificateInvalidError{Reason: x509.Expired}),
		"other x509":        post(x509.SystemRootsError{}),
		"constraint":        post(x509.ConstraintViolationError{}),
		"record header":     post(tls.RecordHeaderError{Msg: "not TLS"}),
		"dns not found":     post(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}),
		"plain error":       post(errors.New("unsupported protocol scheme")),
		"non-timeout net":   post(&net.OpError{Op: "read", Net: "tcp", Err: errors.New("weird")}),
	}
	run := func(name string, cause error, wantCalls int32) {
		synctest.Test(t, func(t *testing.T) {
			var calls atomic.Int32
			c := stubClient(t, stubTransport{systemOne: func(context.Context, *decide.Request) (*decide.Response, error) {
				calls.Add(1)
				return nil, cause
			}})
			_, err := c.SystemOne(t.Context(), valid())
			if !errors.Is(err, cause) && err != cause {
				t.Errorf("%s: last error not returned: %v", name, err)
			}
			if got := calls.Load(); got != wantCalls {
				t.Errorf("%s: %d calls, want %d", name, got, wantCalls)
			}
		})
	}
	for name, err := range retried {
		run(name, err, 3)
	}
	for name, err := range notRetried {
		run(name, err, 1)
	}
}
