package sod

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors. Match them with errors.Is.
var (
	ErrAuth           = errors.New("sod: authentication failed")     // 401, 403
	ErrValidation     = errors.New("sod: request validation failed") // 422
	ErrRateLimited    = errors.New("sod: rate limited")              // 429
	ErrOverloaded     = errors.New("sod: overloaded")                // 529
	ErrServer         = errors.New("sod: server error")              // any 5xx, including 529
	ErrNoAPIKey       = errors.New("sod: no API key: set TYPESAFE_API_KEY or use WithAPIKey")
	ErrInvalidRequest = errors.New("sod: invalid request")
	ErrInvalidAnswer  = errors.New("sod: invalid answer")
	// ErrInconsistentAnswer marks an answer that is well formed but whose
	// numbers disagree with each other beyond tolerance: the choice is not
	// the argmax, the score is outside its range, or the probabilities do not
	// sum to about 1. It is always accompanied by ErrInvalidAnswer.
	ErrInconsistentAnswer = errors.New("sod: inconsistent answer")
	ErrDecode             = errors.New("sod: cannot decode response")
)

// APIError is a non-2xx response from the API.
type APIError struct {
	StatusCode int
	Type       string             // detail.error_type, or Details[0].Type, or ""
	Message    string             // message extracted from the body
	RequestID  string             // x-typesafe-request-id
	RetryAfter time.Duration      // parsed server delay; 0 if absent
	Body       []byte             // raw body, key redacted
	Details    []ValidationDetail // 422 only
}

// ValidationDetail is one entry of a 422 response's "detail" array.
type ValidationDetail struct {
	Loc   []any           `json:"loc"` // strings and numbers (float64)
	Msg   string          `json:"msg"`
	Type  string          `json:"type"`
	Input json.RawMessage `json:"input,omitempty"`
	Ctx   map[string]any  `json:"ctx,omitempty"`
}

// Error returns "sod: <status> <message> (request_id=<id>)".
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("sod: ")
	b.WriteString(strconv.Itoa(e.StatusCode))
	if e.Message != "" {
		b.WriteByte(' ')
		b.WriteString(e.Message)
	}
	if e.RequestID != "" {
		b.WriteString(" (request_id=")
		b.WriteString(e.RequestID)
		b.WriteByte(')')
	}
	return b.String()
}

// Unwrap returns the sentinels for StatusCode: ErrAuth for 401 and 403,
// ErrValidation for 422, ErrRateLimited for 429, ErrOverloaded and ErrServer
// for 529, ErrServer for other 5xx, and none for 408 and other 4xx.
//
// Discrepancy: /api says 401 for a missing or invalid key, but a live
// request without a key returned 403. Both map to ErrAuth. The OpenAPI spec
// lists only 422 as an error response; the docs list 401, 422, 429, and 529.
// We handle all of them.
func (e *APIError) Unwrap() []error {
	switch s := e.StatusCode; {
	case s == http.StatusUnauthorized, s == http.StatusForbidden:
		return []error{ErrAuth}
	case s == http.StatusUnprocessableEntity:
		return []error{ErrValidation}
	case s == http.StatusTooManyRequests:
		return []error{ErrRateLimited}
	case s == 529:
		return []error{ErrOverloaded, ErrServer}
	case s >= 500 && s <= 599:
		return []error{ErrServer}
	}
	return nil
}

// newAPIError builds an *APIError from a response. body must already be
// redacted.
func newAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{
		StatusCode: status,
		RequestID:  header.Get(requestIDHeader),
		Body:       body,
	}
	if d, ok := parseRetryAfter(header, time.Now()); ok {
		e.RetryAfter = d
	}
	e.Type, e.Message, e.Details = parseErrorBody(body)
	e.Message = cmp.Or(e.Message, http.StatusText(status))
	return e
}

// parseErrorBody extracts the type, message, and 422 details from an error
// body. Message extraction mirrors the JS SDK; first match wins: detail
// string; detail.message; detail array; error string; error.message;
// message; else the first 200 bytes of the body.
func parseErrorBody(body []byte) (typ, msg string, details []ValidationDetail) {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return "", truncate(strings.TrimSpace(string(body)), 200), nil
	}
	if raw, ok := m["detail"]; ok {
		var s string
		var obj struct {
			ErrorType string `json:"error_type"`
			Message   string `json:"message"`
		}
		switch {
		case json.Unmarshal(raw, &s) == nil:
			return "", s, nil
		case json.Unmarshal(raw, &details) == nil && details != nil:
			parts := make([]string, 0, len(details))
			for _, d := range details {
				parts = append(parts, formatDetail(d))
			}
			if len(details) > 0 {
				typ = details[0].Type
			}
			return typ, strings.Join(parts, "; "), details
		case json.Unmarshal(raw, &obj) == nil && obj.Message != "":
			return obj.ErrorType, obj.Message, nil
		}
	}
	if raw, ok := m["error"]; ok {
		var s string
		var obj struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &s) == nil {
			return "", s, nil
		}
		if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
			return obj.Type, obj.Message, nil
		}
	}
	if raw, ok := m["message"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return "", s, nil
		}
	}
	return "", truncate(strings.TrimSpace(string(body)), 200), nil
}

// formatDetail renders a 422 detail as "questions.k.criteria: msg", dropping
// the leading "body" location element.
func formatDetail(d ValidationDetail) string {
	parts := make([]string, 0, len(d.Loc))
	for _, l := range d.Loc {
		switch v := l.(type) {
		case string:
			if v == "body" {
				continue
			}
			parts = append(parts, v)
		case float64:
			parts = append(parts, strconv.FormatFloat(v, 'f', -1, 64))
		default:
			parts = append(parts, fmt.Sprint(v))
		}
	}
	if len(parts) == 0 {
		return d.Msg
	}
	return strings.Join(parts, ".") + ": " + d.Msg
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Answer validation reasons, used in AnswerError.Reason.
const (
	ReasonMissingField     = "missing_field"
	ReasonMissingAnswer    = "missing_answer"
	ReasonUnexpectedAnswer = "unexpected_answer"
	ReasonDecodeFailed     = "decode_failed"
	ReasonTypeMismatch     = "type_mismatch"
	ReasonNotFinite        = "not_finite"
	ReasonOutOfRange       = "out_of_range"
	ReasonProbabilityKeys  = "probability_keys"
	ReasonChoiceNotOption  = "choice_not_option"
	ReasonChoiceNotArgmax  = "choice_not_argmax"
	ReasonLegendKeys       = "legend_keys"
	ReasonProbabilitySum   = "probability_sum"
	ReasonCustom           = "custom"
)

// AnswerError reports one answer that failed validation or could not be
// read through a handle.
type AnswerError struct {
	Key    string // question key
	Type   string // question type; for unexpected_answer, the answer's AnswerType()
	Reason string // one of the Reason constants
	Detail string // human-readable specifics, e.g. `choice "x" not in options`
	Err    error  // underlying error for decode_failed and custom

	scoreRange bool // out_of_range on a score's value (check 14)
}

// Error returns `sod: answer "k" (choice): choice_not_option: ...`.
func (e *AnswerError) Error() string {
	var b strings.Builder
	b.WriteString("sod: answer ")
	b.WriteString(strconv.Quote(e.Key))
	if e.Type != "" {
		b.WriteString(" (")
		b.WriteString(e.Type)
		b.WriteByte(')')
	}
	b.WriteString(": ")
	b.WriteString(e.Reason)
	switch {
	case e.Detail != "":
		b.WriteString(": ")
		b.WriteString(e.Detail)
	case e.Err != nil:
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns ErrInvalidAnswer, ErrInconsistentAnswer for consistency
// failures (choice_not_argmax, probability_sum, and a score out of range),
// and Err if non-nil.
func (e *AnswerError) Unwrap() []error {
	errs := []error{ErrInvalidAnswer}
	if e.consistency() {
		errs = append(errs, ErrInconsistentAnswer)
	}
	if e.Err != nil {
		errs = append(errs, e.Err)
	}
	return errs
}

// consistency reports whether the failure is a consistency check (the
// numbers disagree) rather than a structural one (the shape is wrong).
// out_of_range is a consistency failure only for a score's value; a
// probability or confidence out of [0,1] is structural.
func (e *AnswerError) consistency() bool {
	switch e.Reason {
	case ReasonChoiceNotArgmax, ReasonProbabilitySum:
		return true
	case ReasonOutOfRange:
		return e.scoreRange
	}
	return false
}

// InvalidAnswersError collects every answer that failed validation, sorted
// by key then reason. SystemOne returns it together with the response.
type InvalidAnswersError struct{ Answers []*AnswerError }

// Error returns the count plus the first two failures.
func (e *InvalidAnswersError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "sod: %d invalid answer", len(e.Answers))
	if len(e.Answers) != 1 {
		b.WriteByte('s')
	}
	for i, a := range e.Answers {
		if i == 2 {
			b.WriteString("; ...")
			break
		}
		if i == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString("; ")
		}
		b.WriteString(strings.TrimPrefix(a.Error(), "sod: "))
	}
	return b.String()
}

// Unwrap returns each *AnswerError.
func (e *InvalidAnswersError) Unwrap() []error {
	out := make([]error, len(e.Answers))
	for i, a := range e.Answers {
		out[i] = a
	}
	return out
}
