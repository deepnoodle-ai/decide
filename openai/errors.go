package openai

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deepnoodle-ai/decide"
)

// apiError reads OpenAI's {"error": {"message", "type", "param", "code"}}
// body. Type is the error code, or its type when there is no code.
func apiError(status int, header http.Header, raw []byte) *decide.APIError {
	e := &decide.APIError{StatusCode: status, Body: raw,
		RequestID: header.Get(requestIDHeader), RetryAfter: retryAfter(header, time.Now())}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	e.Message = body.Error.Message
	if code, ok := body.Error.Code.(string); ok && code != "" {
		e.Type = code
	} else {
		e.Type = body.Error.Type
	}
	if e.Message == "" && !json.Valid(raw) {
		// Suppressed bodies keep a safe explanation for proxies and outages.
		if msg := strings.TrimSpace(string(raw)); len(msg) <= 200 {
			e.Message = msg
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
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
