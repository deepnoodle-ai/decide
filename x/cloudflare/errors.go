package cloudflare

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deepnoodle-ai/sod"
)

// ErrorDetail is one entry in a Workers AI error array. Body on Error keeps
// all provider fields, including fields not modeled here.
type ErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error reports an unsuccessful Workers AI response. HTTP failures wrap
// *sod.APIError, so errors.As, errors.Is, and client retries still work.
// An unsuccessful 2xx envelope retains its actual status and is not retried.
type Error struct {
	StatusCode int
	Errors     []ErrorDetail
	Body       []byte // redacted response body; malformed/non-JSON bodies suppressed
	Header     http.Header
	apiError   *sod.APIError
}

// Error describes the response status and provider messages.
func (e *Error) Error() string {
	if e.apiError != nil {
		return e.apiError.Error()
	}
	return fmt.Sprintf("sod: cloudflare status %d: %s", e.StatusCode, errorMessage(e.Errors, e.Body))
}

// Unwrap returns the underlying *sod.APIError for an HTTP failure.
func (e *Error) Unwrap() error {
	if e.apiError == nil {
		return nil
	}
	return e.apiError
}

func providerError(status int, header http.Header, raw []byte, httpFailure bool) *Error {
	e := &Error{StatusCode: status, Body: raw, Header: header}
	var body struct {
		Errors []ErrorDetail `json:"errors"`
	}
	_ = json.Unmarshal(raw, &body)
	e.Errors = body.Errors
	if httpFailure {
		message := errorMessage(e.Errors, raw)
		if message == "Workers AI reported an unsuccessful response" {
			message = http.StatusText(status)
		}
		e.apiError = &sod.APIError{StatusCode: status, Message: message,
			Body: raw, RetryAfter: retryAfter(header, time.Now())}
		if len(e.Errors) > 0 {
			e.apiError.Type = strconv.Itoa(e.Errors[0].Code)
		}
	}
	return e
}

func errorMessage(details []ErrorDetail, raw []byte) string {
	var parts []string
	for _, d := range details {
		parts = append(parts, fmt.Sprintf("%d: %s", d.Code, d.Message))
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	// Suppressed bodies retain a safe explanation for proxies and outages.
	if !json.Valid(raw) {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg != "" {
			return msg
		}
	}
	return "Workers AI reported an unsuccessful response"
}

func retryAfter(h http.Header, now time.Time) time.Duration {
	if v := h.Get("retry-after-ms"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 && !math.IsNaN(n) {
			return duration(n, time.Millisecond)
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 && !math.IsNaN(n) {
		return duration(n, time.Second)
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

func duration(n float64, unit time.Duration) time.Duration {
	if ns := n * float64(unit); ns >= math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	} else {
		return time.Duration(ns)
	}
}
