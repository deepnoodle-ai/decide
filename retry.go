package decide

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxServerDelay = 60 * time.Second

// parseRetryAfter reads the server's requested delay. retry-after-ms (a
// non-negative number of milliseconds) wins; otherwise Retry-After as
// non-negative seconds (integer or decimal) or an HTTP date, clamped at 0.
func parseRetryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	if v := strings.TrimSpace(h.Get("retry-after-ms")); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms >= 0 && !math.IsNaN(ms) {
			return clampDuration(ms, time.Millisecond), true
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil {
		if s < 0 || math.IsNaN(s) {
			return 0, false
		}
		return clampDuration(s, time.Second), true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

// clampDuration converts n units to a Duration. A value that would exceed
// math.MaxInt64 nanoseconds (for example Retry-After: 1e10 or +Inf) becomes
// math.MaxInt64, which is over 60 s and so falls back to backoff; a plain
// conversion would overflow and go negative.
func clampDuration(n float64, unit time.Duration) time.Duration {
	ns := n * float64(unit)
	if ns >= math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ns)
}

// backoff returns min(initial * 2^k, max) * (1 - r*0.25), where r is in
// [0,1). This matches the JS SDK's formula.
func backoff(k int, initial, maxDelay time.Duration, r float64) time.Duration {
	d := maxDelay
	// initial <= maxDelay>>k guarantees initial<<k cannot wrap.
	if k < 62 && initial <= maxDelay>>k {
		d = initial << k
	}
	// Subtract the jitter rather than scaling d through float64, which
	// rounds up near math.MaxInt64 and would overflow.
	return d - time.Duration(float64(d)*r*0.25)
}

// retryDelay picks the delay before retry k (zero-based). A server delay in
// (0, 60s] wins; anything else falls back to backoff. APIError.RetryAfter is
// 0 when the header is absent, so a server delay of exactly 0 also falls
// back to backoff; the JS SDK does the same.
func (c *Client) retryDelay(err error, k int) time.Duration {
	if ae, ok := errors.AsType[*APIError](err); ok && ae.RetryAfter > 0 && ae.RetryAfter <= maxServerDelay {
		return ae.RetryAfter
	}
	return backoff(k, c.initialBackoff, c.maxBackoff, rand.Float64())
}

// retryable reports whether err from one attempt should be retried. ctx is
// the caller's context, not the per-attempt one.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if ae, ok := errors.AsType[*APIError](err); ok {
		s := ae.StatusCode
		return s == http.StatusRequestTimeout || s == http.StatusTooManyRequests || (s >= 500 && s <= 599)
	}
	if errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrDecode) {
		return false
	}
	// Errors that retrying cannot fix: bad certificates, a TLS handshake
	// with something that is not TLS, and a host name that does not exist.
	// *url.Error implements net.Error for every http.Client failure, so these
	// must be ruled out before the net.Error check below.
	if hasX509Error(err) {
		return false
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return false
	}
	if dns, ok := errors.AsType[*net.DNSError](err); ok && dns.IsNotFound {
		return false
	}
	// The caller's context is live, so a deadline here is the per-attempt one.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return true
	}
	if op, ok := errors.AsType[*net.OpError](err); ok && op.Op == "dial" {
		return true
	}
	// net/http has no exported sentinel for the keep-alive race where the
	// server closes an idle connection just as a request is written; its
	// transport.go defines errServerClosedIdle with this text. Matching the
	// text is the only option; the JS SDK retries every connection error.
	if strings.Contains(err.Error(), "http: server closed idle connection") {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}

// hasX509Error reports whether err's tree holds an error type from
// crypto/x509. The list is every exported error type in crypto/x509 as of
// Go 1.27, matched by type rather than by name, so the check is specific to
// that package and needs no reflect import. Review it when Go adds one.
func hasX509Error(err error) bool {
	for _, is := range []func(error) bool{
		isType[x509.CertificateInvalidError],
		isType[x509.ConstraintViolationError],
		isType[x509.HostnameError],
		isType[x509.InsecureAlgorithmError],
		isType[x509.SystemRootsError],
		isType[x509.UnhandledCriticalExtension],
		isType[x509.UnknownAuthorityError],
	} {
		if is(err) {
			return true
		}
	}
	return false
}

func isType[E error](err error) bool {
	_, ok := errors.AsType[E](err)
	return ok
}

// retryPolicy is the per-call view of the client's retry settings.
type retryPolicy struct {
	maxRetries     int
	attemptTimeout time.Duration
}

// withRetry runs call up to 1+maxRetries times. Each attempt gets a context
// carrying its attempt number and, if set, the per-attempt timeout. logf is
// called after every attempt. When retries are exhausted, the last error is
// returned unchanged so errors.Is still works on it.
//
// When the caller's context ends during an attempt or a sleep, the result is
// errors.Join(ctx.Err(), lastErr), where lastErr is the most recent attempt
// that failed before ctx ended, or ctx.Err() alone if none had.
func withRetry[T any](ctx context.Context, c *Client, p retryPolicy, op string,
	call func(context.Context) (T, error),
	logf func(attempt int, d time.Duration, v T, err error),
) (T, error) {
	var zero T
	var lastErr error
	cancelled := func() (T, error) {
		if lastErr == nil {
			return zero, ctx.Err()
		}
		return zero, errors.Join(ctx.Err(), lastErr)
	}
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return cancelled()
		}
		actx := withAttempt(ctx, attempt)
		cancel := context.CancelFunc(func() {})
		if p.attemptTimeout > 0 {
			actx, cancel = context.WithTimeout(actx, p.attemptTimeout)
		}
		start := time.Now()
		v, err := call(actx)
		cancel()
		logf(attempt, time.Since(start), v, err)
		if err == nil {
			return v, nil
		}
		if ctx.Err() != nil {
			return cancelled()
		}
		lastErr = err
		if !retryable(ctx, err) {
			return v, err
		}
		exhausted := func() (T, error) {
			c.logger.LogAttrs(ctx, slog.LevelWarn, "decide retries exhausted",
				slog.String("op", op),
				slog.Int("attempts", attempt+1),
				slog.String("error", err.Error()),
				slog.String("request_id", requestIDOf(err)),
			)
			return v, err
		}
		if attempt >= p.maxRetries {
			return exhausted()
		}
		delay := c.retryDelay(err, attempt)
		if dl, ok := ctx.Deadline(); ok && time.Now().Add(delay).After(dl) {
			return exhausted() // the deadline leaves no room for another attempt
		}
		c.logger.LogAttrs(ctx, slog.LevelInfo, "decide retry",
			slog.String("op", op),
			slog.Int("attempt", attempt+1),
			slog.Duration("delay", delay),
			slog.String("reason", retryReason(err)),
			slog.String("request_id", requestIDOf(err)),
		)
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return cancelled()
		}
	}
}

// retryReason is the HTTP status for an *APIError, else the error text.
func retryReason(err error) string {
	if ae, ok := errors.AsType[*APIError](err); ok {
		return strconv.Itoa(ae.StatusCode)
	}
	return err.Error()
}

func requestIDOf(err error) string {
	if ae, ok := errors.AsType[*APIError](err); ok {
		return ae.RequestID
	}
	return ""
}
