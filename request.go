package sod

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
)

// Request is one call to POST /v1/systemone.
//
// A Request is not safe for concurrent mutation. A fully built request may
// be sent concurrently by many goroutines; the client never mutates it.
type Request struct {
	State     any    // string, object, array, or json.RawMessage
	Model     string // empty: client default
	Questions map[string]Question
	Extra     map[string]any // unmodeled top-level fields
}

// RequestOption configures a Request built by NewRequest.
type RequestOption func(*Request)

// NewRequest returns a request for state with a non-nil Questions map.
func NewRequest(state any, opts ...RequestOption) *Request {
	r := &Request{State: state, Questions: make(map[string]Question)}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithRequestModel sets the model for this request, overriding the client
// default. Aliases such as "jev-latest" and versioned IDs are both accepted.
func WithRequestModel(name string) RequestOption {
	return func(r *Request) { r.Model = name }
}

// WithRequestExtra sets an unmodeled top-level request field.
func WithRequestExtra(key string, value any) RequestOption {
	return func(r *Request) {
		if r.Extra == nil {
			r.Extra = make(map[string]any)
		}
		r.Extra[key] = value
	}
}

// MarshalJSON writes state, model (omitted when empty), questions sorted by
// key, then Extra sorted by key.
func (r *Request) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("state", r.State)
	if r.Model != "" {
		w.field("model", r.Model)
	}
	q := newObjectWriter()
	for _, k := range slices.Sorted(maps.Keys(r.Questions)) {
		if r.Questions[k] == nil {
			return nil, fmt.Errorf("sod: question %q is nil", k)
		}
		q.field(k, r.Questions[k])
	}
	qb, err := q.bytes()
	if err != nil {
		return nil, err
	}
	w.rawField("questions", qb)
	w.extra(r.Extra, "state", "model", "questions")
	return w.bytes()
}

// UnmarshalJSON decodes a request. Questions are decoded with
// DecodeQuestion; unknown fields go to Extra with their raw bytes. A string
// state decodes to a Go string; any other state is kept as json.RawMessage
// so its bytes, including object member order, survive.
func (r *Request) UnmarshalJSON(data []byte) error {
	m, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("sod: decode request: %w", err)
	}
	*r = Request{Questions: make(map[string]Question), Extra: extraFrom(m, "state", "model", "questions")}
	if raw, ok := m["state"]; ok && !isNull(raw) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			r.State = s
		} else {
			r.State = append(json.RawMessage(nil), raw...)
		}
	}
	if raw, ok := m["model"]; ok && !isNull(raw) {
		if err := json.Unmarshal(raw, &r.Model); err != nil {
			return fmt.Errorf("sod: decode request model: %w", err)
		}
	}
	if raw, ok := m["questions"]; ok && !isNull(raw) {
		qs, err := decodeObject(raw)
		if err != nil {
			return fmt.Errorf("sod: decode request questions: %w", err)
		}
		for k, qraw := range qs {
			q, err := DecodeQuestion(qraw)
			if err != nil {
				return fmt.Errorf("sod: question %q: %w", k, err)
			}
			r.Questions[k] = q
		}
	}
	return nil
}

// Validate reports whether the request can be sent. Errors wrap
// ErrInvalidRequest. It does not enforce per-model limits such as the
// 255-option or 10-level caps; the server reports those as 422.
func (r *Request) Validate() error {
	if r == nil {
		return invalidRequest("request is nil")
	}
	// Discrepancy: the OpenAPI spec forbids a null state, while the JS SDK
	// type allows it. We refuse nil state, including a typed nil that
	// encodes to null (strict in what we send).
	if r.State == nil {
		return invalidRequest("state is nil")
	}
	if err := checkBytes("state", r.State); err != nil {
		return err
	}
	if b, err := json.Marshal(r.State); err != nil {
		return fmt.Errorf("%w: state: %w", ErrInvalidRequest, err)
	} else if isNull(b) {
		return invalidRequest("state encodes to null")
	}
	if len(r.Questions) == 0 {
		return invalidRequest("no questions")
	}
	for _, k := range slices.Sorted(maps.Keys(r.Questions)) {
		if err := validateQuestion(k, r.Questions[k]); err != nil {
			return err
		}
	}
	for _, k := range []string{"state", "model", "questions"} {
		if _, ok := r.Extra[k]; ok {
			return invalidRequest(fmt.Sprintf("Extra key %q collides with a modeled field", k))
		}
	}
	return nil
}

func validateQuestion(k string, q Question) error {
	if k == "" {
		return invalidRequest("a question key is empty")
	}
	if q == nil || isNilQuestion(q) {
		return invalidRequest(fmt.Sprintf("question %q is nil", k))
	}
	where := func(field string) string { return fmt.Sprintf("question %q %s", k, field) }
	switch q := q.(type) {
	case *NoulQuestion:
		if err := checkBytes(where("instructions"), q.Instructions); err != nil {
			return err
		}
		if q.Criteria != nil {
			if err := checkBytes(where("criteria.true"), q.Criteria.True); err != nil {
				return err
			}
			if err := checkBytes(where("criteria.false"), q.Criteria.False); err != nil {
				return err
			}
		}
	case *ChoiceQuestion:
		if err := checkBytes(where("instructions"), q.Instructions); err != nil {
			return err
		}
		if len(q.Criteria) == 0 {
			return invalidRequest(fmt.Sprintf("choice question %q has no options", k))
		}
		for _, o := range q.Criteria {
			if err := checkBytes(where(fmt.Sprintf("option %q", o.Key)), o.Description); err != nil {
				return err
			}
		}
	case *ScoreQuestion:
		if err := checkBytes(where("instructions"), q.Instructions); err != nil {
			return err
		}
		// Discrepancy: the docs say a score needs at least two levels, the
		// OpenAPI spec says minItems 1, the JS SDK enforces two, and the
		// Python SDK requires non-empty. We enforce two when sending and
		// accept any count when decoding.
		if len(q.Criteria) < 2 {
			return invalidRequest(fmt.Sprintf("score question %q has %d levels; at least 2 are required", k, len(q.Criteria)))
		}
		for i, l := range q.Criteria {
			if err := checkBytes(where(fmt.Sprintf("level %d", i)), l); err != nil {
				return err
			}
		}
	}
	// Encoding catches empty and repeated option keys, Extra collisions,
	// and malformed raw questions, with the same rules the wire uses.
	if err := marshalQuestion(q); err != nil {
		return fmt.Errorf("%w: question %q: %w", ErrInvalidRequest, k, err)
	}
	return nil
}

// isNilQuestion reports a nil pointer of a built-in question type, which
// passes a q == nil check but would panic when encoded.
func isNilQuestion(q Question) bool {
	switch q := q.(type) {
	case *NoulQuestion:
		return q == nil
	case *ChoiceQuestion:
		return q == nil
	case *ScoreQuestion:
		return q == nil
	case *RawQuestion:
		return q == nil
	}
	return false
}

// marshalQuestion encodes q, turning a panic into an error. A typed-nil
// pointer of a question type from another package cannot be detected
// without reflection, but its MarshalJSON panicking must not crash the
// caller.
func marshalQuestion(q Question) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("MarshalJSON panicked: %v", r)
		}
	}()
	_, err = q.MarshalJSON()
	return err
}

// checkBytes rejects a []byte value, which encoding/json would send as
// base64. It looks at v and, for map[string]any and []any, at v's direct
// elements; deeper nesting is the caller's problem.
func checkBytes(where string, v any) error {
	bad := func() error {
		return invalidRequest(where + " is []byte, which encodes as base64; pass a string or json.RawMessage")
	}
	switch v := v.(type) {
	case []byte:
		return bad()
	case map[string]any:
		for _, e := range v {
			if _, ok := e.([]byte); ok {
				return bad()
			}
		}
	case []any:
		for _, e := range v {
			if _, ok := e.([]byte); ok {
				return bad()
			}
		}
	}
	return nil
}

func invalidRequest(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, msg)
}

// Response is the result of POST /v1/systemone.
type Response struct {
	Model     string // resolved version, e.g. "jev-1.13.0"
	Answers   map[string]Answer
	Usage     Usage
	RequestID string                     // x-typesafe-request-id
	Header    http.Header                // response headers; nil if the transport has none
	Raw       []byte                     // response body; nil if the transport has none
	Extra     map[string]json.RawMessage // unmodeled top-level members

	// Invalid holds the validation failure for each key that failed, set by
	// the client after validation; nil when every answer is valid.
	Invalid map[string]*AnswerError
}

// Usage reports token counts for a request.
type Usage struct {
	InputTokens  int                        // "input_tokens"
	OutputTokens int                        // "output_tokens"
	Extra        map[string]json.RawMessage // unmodeled members, written back on marshal
}

// MarshalJSON writes input_tokens, output_tokens, then Extra sorted by key.
// It has a value receiver so a Usage field marshals without being addressable.
func (u Usage) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("input_tokens", u.InputTokens)
	w.field("output_tokens", u.OutputTokens)
	w.rawExtra(u.Extra, "input_tokens", "output_tokens")
	return w.bytes()
}

// UnmarshalJSON decodes usage leniently. Token counts are read as numbers
// and kept when integral (so 12.0 is 12); a count that is not an integral
// number is left at zero and its raw bytes go to Extra, as do unknown
// members. It fails only if data is not a JSON object.
func (u *Usage) UnmarshalJSON(data []byte) error {
	m, err := decodeObject(data)
	if err != nil {
		return fmt.Errorf("sod: decode usage: %w", err)
	}
	v := Usage{}
	for k, raw := range m {
		var dst *int
		switch k {
		case "input_tokens":
			dst = &v.InputTokens
		case "output_tokens":
			dst = &v.OutputTokens
		}
		if dst != nil && isNull(raw) {
			continue
		}
		if n, ok := integral(raw); dst != nil && ok {
			*dst = n
			continue
		}
		if v.Extra == nil {
			v.Extra = make(map[string]json.RawMessage)
		}
		v.Extra[k] = raw
	}
	*u = v
	return nil
}

// integral decodes raw as a JSON number with an integral value no larger
// than 2^53, the range where float64 holds integers exactly.
func integral(raw json.RawMessage) (int, bool) {
	var f float64
	if json.Unmarshal(raw, &f) != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int(f), true
}

// UnmarshalJSON decodes a response using the answer registry only; see
// DecodeResponse to decode with the request's question types.
func (r *Response) UnmarshalJSON(data []byte) error {
	resp, err := DecodeResponse(data, nil)
	if err != nil {
		return err
	}
	*r = *resp
	return nil
}

// DecodeResponse decodes a response body using req to choose answer types,
// then the registry, then *RawAnswer. req may be nil. It fails only if data
// is not a JSON object or "answers" is not an object; a bad answer never
// fails the whole response.
//
// Decoding is lenient: unknown top-level members go to Extra, a missing
// "answers" becomes an empty map, and a missing "usage" leaves zeros. If
// "model" or "usage" has the wrong JSON type, the field is left zero and its
// raw bytes are kept in Extra under the same key; MarshalJSON does not write
// them back, because the modeled field wins.
//
// Discrepancy: usage is required by the docs and the OpenAPI spec but
// optional in the Python SDK. We decode a missing usage as zeros.
func DecodeResponse(data []byte, req *Request) (*Response, error) {
	m, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("sod: decode response: %w", err)
	}
	r := &Response{Answers: make(map[string]Answer)}
	for k, v := range m {
		switch k {
		case "model", "answers", "usage":
		default:
			if r.Extra == nil {
				r.Extra = make(map[string]json.RawMessage)
			}
			r.Extra[k] = v
		}
	}
	keepRaw := func(k string, raw json.RawMessage) {
		if r.Extra == nil {
			r.Extra = make(map[string]json.RawMessage)
		}
		r.Extra[k] = raw
	}
	if raw, ok := m["model"]; ok && !isNull(raw) {
		if json.Unmarshal(raw, &r.Model) != nil {
			r.Model = ""
			keepRaw("model", raw)
		}
	}
	if raw, ok := m["answers"]; ok && !isNull(raw) {
		as, err := decodeObject(raw)
		if err != nil {
			return nil, fmt.Errorf("sod: decode response answers: %w", err)
		}
		for k, araw := range as {
			var q Question
			if req != nil {
				q = req.Questions[k]
			}
			a, _ := decodeAnswer(araw, q) // failures are kept as *RawAnswer
			r.Answers[k] = a
		}
	}
	if raw, ok := m["usage"]; ok && !isNull(raw) {
		if r.Usage.UnmarshalJSON(raw) != nil {
			r.Usage = Usage{}
			keepRaw("usage", raw)
		}
	}
	return r, nil
}

// MarshalJSON writes the wire form: model, answers sorted by key, usage,
// then Extra sorted by key. RequestID, Header, Raw, and Invalid are not part
// of the body and are not written.
func (r *Response) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("model", r.Model)
	a := newObjectWriter()
	for _, k := range slices.Sorted(maps.Keys(r.Answers)) {
		if r.Answers[k] == nil {
			return nil, fmt.Errorf("sod: answer %q is nil", k)
		}
		a.field(k, r.Answers[k])
	}
	ab, err := a.bytes()
	if err != nil {
		return nil, err
	}
	w.rawField("answers", ab)
	w.field("usage", r.Usage)
	w.rawExtra(r.Extra, "model", "answers", "usage")
	return w.bytes()
}
