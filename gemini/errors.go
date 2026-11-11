package gemini

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide"
)

// apiError reads Google's {"error": {"code", "message", "status",
// "details"}} body. Type is the first detail's reason, such as
// API_KEY_INVALID, then a string error code, such as "safety", then the
// status, such as INVALID_ARGUMENT.
func apiError(status int, header http.Header, raw []byte) error {
	e := &decide.APIError{StatusCode: status, Body: raw,
		RequestID: header.Get(requestIDHeader), RetryAfter: retryAfter(header, time.Now())}
	var body struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	e.Message = body.Error.Message
	for _, d := range body.Error.Details {
		if d.Reason != "" {
			e.Type = d.Reason
			break
		}
	}
	if code, ok := body.Error.Code.(string); ok && code != "" && e.Type == "" {
		e.Type = code
	}
	if e.Type == "" {
		e.Type = body.Error.Status
	}
	// A body that is not Google's error, such as a proxy's page, was
	// suppressed before this point; the status says what happened.
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	// Google answers a bad key with 400, not 401. Report it as an
	// authentication failure too, so callers and the CLI can tell.
	if status == http.StatusBadRequest && (e.Type == "API_KEY_INVALID" || e.Type == "API_KEY_EXPIRED") {
		return &authError{e}
	}
	return e
}

// authError is an APIError that is also decide.ErrAuth.
type authError struct{ *decide.APIError }

func (e *authError) Unwrap() []error { return []error{e.APIError, decide.ErrAuth} }

func retryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 && !math.IsNaN(n) {
		if ns := n * float64(time.Second); ns >= math.MaxInt64 {
			return time.Duration(math.MaxInt64)
		} else {
			return time.Duration(ns)
		}
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}
