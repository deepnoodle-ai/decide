package sod

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Question is one entry in Request.Questions. Implementations outside this
// package are allowed. MarshalJSON must emit a JSON object whose "type"
// member equals QuestionType().
type Question interface {
	QuestionType() string
	json.Marshaler
}

// QuestionFor binds a question to the Go type of its answer so Ask can infer
// the handle type. NewAnswer returns a new zero answer.
type QuestionFor[A Answer] interface {
	Question
	NewAnswer() A
}

// AnswerMaker is optionally implemented by a Question so responses decode
// into the question's own answer type without registering it. MakeAnswer
// returns a new zero answer; it is the non-generic twin of
// QuestionFor.NewAnswer. When a response is decoded with its request, an
// answer whose wire type equals the question's type is made by MakeAnswer,
// before the registry is consulted.
type AnswerMaker interface {
	MakeAnswer() Answer
}

// AnswerValidator is optionally implemented by a Question. The client calls
// it during validation after the generic checks (answer present, no
// unexpected answers, decodable, matching type).
type AnswerValidator interface {
	ValidateAnswer(a Answer) error
}

// Wire names of the built-in question and answer types.
const (
	typeNoul   = "noul"
	typeChoice = "choice"
	typeScore  = "score"
)

// NoulQuestion asks whether a condition holds. Its answer is P(yes).
//
// Instructions and the criteria values may be a string, an object, an array,
// or nil (JSON null); they are encoded with encoding/json as given.
type NoulQuestion struct {
	Instructions any
	Criteria     *NoulCriteria // nil omits "criteria"
	Extra        map[string]any
}

// NoulCriteria optionally describes what "true" and "false" mean.
type NoulCriteria struct {
	True  any // encoded as "true"; nil encodes as null (the spec allows it)
	False any // encoded as "false"
}

// ChoiceQuestion asks which one of a set of options applies.
type ChoiceQuestion struct {
	Instructions any
	Criteria     []ChoiceOption // encoded as a JSON object in slice order
	Extra        map[string]any
}

// ChoiceOption is one option of a ChoiceQuestion. The model sees both the
// key and the description.
type ChoiceOption struct {
	Key         string
	Description any // nil encodes as null: "interpreted by its name alone"
}

// ScoreQuestion asks where on an ordered scale the state falls.
type ScoreQuestion struct {
	Instructions any
	Criteria     []any // ordered levels; index is the level number
	Extra        map[string]any
}

// RawQuestion sends a question type this package does not model.
type RawQuestion struct {
	Type string
	JSON json.RawMessage // the full question object, including "type"
}

// QuestionType returns "noul".
func (q *NoulQuestion) QuestionType() string { return typeNoul }

// QuestionType returns "choice".
func (q *ChoiceQuestion) QuestionType() string { return typeChoice }

// QuestionType returns "score".
func (q *ScoreQuestion) QuestionType() string { return typeScore }

// QuestionType returns q.Type.
func (q *RawQuestion) QuestionType() string { return q.Type }

// NewAnswer returns a new *NoulAnswer.
func (q *NoulQuestion) NewAnswer() *NoulAnswer { return &NoulAnswer{} }

// NewAnswer returns a new *ChoiceAnswer.
func (q *ChoiceQuestion) NewAnswer() *ChoiceAnswer { return &ChoiceAnswer{} }

// NewAnswer returns a new *ScoreAnswer.
func (q *ScoreQuestion) NewAnswer() *ScoreAnswer { return &ScoreAnswer{} }

// NewAnswer returns a new *RawAnswer with Type set.
func (q *RawQuestion) NewAnswer() *RawAnswer { return &RawAnswer{Type: q.Type} }

// MakeAnswer returns q.NewAnswer().
func (q *NoulQuestion) MakeAnswer() Answer { return q.NewAnswer() }

// MakeAnswer returns q.NewAnswer().
func (q *ChoiceQuestion) MakeAnswer() Answer { return q.NewAnswer() }

// MakeAnswer returns q.NewAnswer().
func (q *ScoreQuestion) MakeAnswer() Answer { return q.NewAnswer() }

// MakeAnswer returns q.NewAnswer().
func (q *RawQuestion) MakeAnswer() Answer { return q.NewAnswer() }

// Builders.

// NoulOption configures a NoulQuestion built by Noul.
type NoulOption func(*NoulQuestion)

// Noul builds a Noul question.
func Noul(instructions any, opts ...NoulOption) *NoulQuestion {
	q := &NoulQuestion{Instructions: instructions}
	for _, o := range opts {
		o(q)
	}
	return q
}

// NoulTrue sets Criteria.True, allocating Criteria.
func NoulTrue(description any) NoulOption {
	return func(q *NoulQuestion) {
		if q.Criteria == nil {
			q.Criteria = &NoulCriteria{}
		}
		q.Criteria.True = description
	}
}

// NoulFalse sets Criteria.False, allocating Criteria.
func NoulFalse(description any) NoulOption {
	return func(q *NoulQuestion) {
		if q.Criteria == nil {
			q.Criteria = &NoulCriteria{}
		}
		q.Criteria.False = description
	}
}

// Choice builds a Choice question. Options are sent in the order given.
func Choice(instructions any, options ...ChoiceOption) *ChoiceQuestion {
	return &ChoiceQuestion{Instructions: instructions, Criteria: options}
}

// Option builds a ChoiceOption. Zero descriptions means nil (null); more than
// one panics, because the variadic exists only to make the description optional.
func Option(key string, description ...any) ChoiceOption {
	switch len(description) {
	case 0:
		return ChoiceOption{Key: key}
	case 1:
		return ChoiceOption{Key: key, Description: description[0]}
	default:
		panic("sod: Option takes at most one description")
	}
}

// OptionsFromMap converts a map to options sorted by key, for callers who
// hold criteria as a map and want deterministic order.
func OptionsFromMap[V any](m map[string]V) []ChoiceOption {
	out := make([]ChoiceOption, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, ChoiceOption{Key: k, Description: m[k]})
	}
	return out
}

// Score builds a Score question. levels are ordered; the index of each level
// is its number.
func Score(instructions any, levels ...any) *ScoreQuestion {
	return &ScoreQuestion{Instructions: instructions, Criteria: levels}
}

// ScoreOf is Score for a typed slice, e.g. ScoreOf("How urgent?", []string{...}).
func ScoreOf[T any](instructions any, levels []T) *ScoreQuestion {
	crit := make([]any, len(levels))
	for i, l := range levels {
		crit[i] = l
	}
	return &ScoreQuestion{Instructions: instructions, Criteria: crit}
}

// Encoding.
//
// Discrepancy: the docs call "instructions" required, while the OpenAPI spec
// (0.2.0) makes it optional and nullable. We send whatever the caller set,
// including null, and do not require it.

// MarshalJSON encodes the question with "type" first.
func (q *NoulQuestion) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("type", typeNoul)
	w.field("instructions", q.Instructions)
	if q.Criteria != nil {
		c := newObjectWriter()
		c.field("true", q.Criteria.True)
		c.field("false", q.Criteria.False)
		b, err := c.bytes()
		if err != nil {
			return nil, err
		}
		w.rawField("criteria", b)
	}
	w.extra(q.Extra, "type", "instructions", "criteria")
	return w.bytes()
}

// MarshalJSON encodes the question with options in slice order. It fails if
// an option key is empty or repeated.
func (q *ChoiceQuestion) MarshalJSON() ([]byte, error) {
	crit, err := encodeOptions(q.Criteria)
	if err != nil {
		return nil, err
	}
	w := newObjectWriter()
	w.field("type", typeChoice)
	w.field("instructions", q.Instructions)
	w.rawField("criteria", crit)
	w.extra(q.Extra, "type", "instructions", "criteria")
	return w.bytes()
}

// encodeOptions writes options as a JSON object in slice order with a
// jsontext.Encoder, which also rejects duplicate names.
func encodeOptions(opts []ChoiceOption) ([]byte, error) {
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(opts))
	for _, o := range opts {
		if o.Key == "" {
			return nil, errors.New("sod: choice option key is empty")
		}
		if seen[o.Key] {
			return nil, fmt.Errorf("sod: choice option key %q is repeated", o.Key)
		}
		seen[o.Key] = true
		d, err := json.Marshal(o.Description)
		if err != nil {
			return nil, fmt.Errorf("sod: choice option %q: %w", o.Key, err)
		}
		if err := enc.WriteToken(jsontext.String(o.Key)); err != nil {
			return nil, err
		}
		if err := enc.WriteValue(d); err != nil {
			return nil, err
		}
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil // the encoder ends top-level values with a newline
}

// MarshalJSON encodes the question with levels in order.
//
// Discrepancy: the OpenAPI spec forbids null level entries (Choice allows
// null descriptions), while the JS builder docs say entries may be null. We
// send what the caller gave and let the server decide.
func (q *ScoreQuestion) MarshalJSON() ([]byte, error) {
	levels := q.Criteria
	if levels == nil {
		levels = []any{}
	}
	w := newObjectWriter()
	w.field("type", typeScore)
	w.field("instructions", q.Instructions)
	w.field("criteria", levels)
	w.extra(q.Extra, "type", "instructions", "criteria")
	return w.bytes()
}

// MarshalJSON returns q.JSON unchanged after checking that it is an object
// whose "type" equals q.Type.
func (q *RawQuestion) MarshalJSON() ([]byte, error) {
	typ, _, err := readType(q.JSON)
	if err != nil {
		return nil, fmt.Errorf("sod: raw question: %w", err)
	}
	if typ != q.Type {
		return nil, fmt.Errorf("sod: raw question: JSON type %q does not match Type %q", typ, q.Type)
	}
	return q.JSON, nil
}

// Decoding.

// UnmarshalJSON decodes a noul question. Unknown members go to Extra.
func (q *NoulQuestion) UnmarshalJSON(data []byte) error {
	m, err := decodeQuestionObject(data, typeNoul)
	if err != nil {
		return err
	}
	*q = NoulQuestion{Extra: extraFrom(m, "type", "instructions", "criteria")}
	if q.Instructions, err = decodeAny(m["instructions"]); err != nil {
		return err
	}
	if raw, ok := m["criteria"]; ok && !isNull(raw) {
		cm, err := decodeObject(raw)
		if err != nil {
			return fmt.Errorf("sod: noul criteria: %w", err)
		}
		q.Criteria = &NoulCriteria{}
		if q.Criteria.True, err = decodeAny(cm["true"]); err != nil {
			return err
		}
		if q.Criteria.False, err = decodeAny(cm["false"]); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalJSON decodes a choice question, preserving option order.
func (q *ChoiceQuestion) UnmarshalJSON(data []byte) error {
	m, err := decodeQuestionObject(data, typeChoice)
	if err != nil {
		return err
	}
	*q = ChoiceQuestion{Extra: extraFrom(m, "type", "instructions", "criteria")}
	if q.Instructions, err = decodeAny(m["instructions"]); err != nil {
		return err
	}
	if raw, ok := m["criteria"]; ok && !isNull(raw) {
		members, err := decodeOrderedObject(raw) // jsontext, so wire order survives
		if err != nil {
			return fmt.Errorf("sod: choice criteria: %w", err)
		}
		q.Criteria = make([]ChoiceOption, 0, len(members))
		for _, mem := range members {
			d, err := decodeAny(mem.Value)
			if err != nil {
				return err
			}
			q.Criteria = append(q.Criteria, ChoiceOption{Key: mem.Key, Description: d})
		}
	}
	return nil
}

// UnmarshalJSON decodes a score question. Any number of levels is accepted.
func (q *ScoreQuestion) UnmarshalJSON(data []byte) error {
	m, err := decodeQuestionObject(data, typeScore)
	if err != nil {
		return err
	}
	*q = ScoreQuestion{Extra: extraFrom(m, "type", "instructions", "criteria")}
	if q.Instructions, err = decodeAny(m["instructions"]); err != nil {
		return err
	}
	if raw, ok := m["criteria"]; ok && !isNull(raw) {
		var levels []json.RawMessage
		if err := json.Unmarshal(raw, &levels); err != nil {
			return fmt.Errorf("sod: score criteria: %w", err)
		}
		q.Criteria = make([]any, len(levels))
		for i, l := range levels {
			if q.Criteria[i], err = decodeAny(l); err != nil {
				return err
			}
		}
	}
	return nil
}

// UnmarshalJSON keeps a copy of data and reads Type from it.
func (q *RawQuestion) UnmarshalJSON(data []byte) error {
	typ, _, err := readType(data)
	if err != nil {
		return fmt.Errorf("sod: raw question: %w", err)
	}
	q.Type = typ
	q.JSON = append(json.RawMessage(nil), data...)
	return nil
}

// DecodeQuestion decodes one question object. Unknown types become *RawQuestion.
func DecodeQuestion(data []byte) (Question, error) {
	typ, _, err := readType(data)
	if err != nil {
		return nil, fmt.Errorf("sod: decode question: %w", err)
	}
	var q interface {
		Question
		json.Unmarshaler
	}
	switch typ {
	case typeNoul:
		q = &NoulQuestion{}
	case typeChoice:
		q = &ChoiceQuestion{}
	case typeScore:
		q = &ScoreQuestion{}
	default:
		q = &RawQuestion{}
	}
	if err := q.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return q, nil
}

// decodeQuestionObject decodes a question object and checks its type.
func decodeQuestionObject(data []byte, want string) (map[string]json.RawMessage, error) {
	typ, m, err := readType(data)
	if err != nil {
		return nil, fmt.Errorf("sod: decode %s question: %w", want, err)
	}
	if typ != want {
		return nil, fmt.Errorf("sod: decode %s question: type is %q", want, typ)
	}
	return m, nil
}

// readType decodes data as an object and returns its string "type" member.
func readType(data []byte) (string, map[string]json.RawMessage, error) {
	m, err := decodeObject(data)
	if err != nil {
		return "", nil, err
	}
	raw, ok := m["type"]
	if !ok {
		return "", m, errors.New(`missing "type"`)
	}
	var typ string
	if err := json.Unmarshal(raw, &typ); err != nil {
		return "", m, errors.New(`"type" is not a string`)
	}
	return typ, m, nil
}

// Handle is a typed reference to one question in a request. It reads the
// matching answer from a response as the answer's concrete type.
type Handle[A Answer] struct {
	key   string
	qtype string
}

// Key returns the question key.
func (h Handle[A]) Key() string { return h.key }

// From returns the answer for h's key. In order: if resp is nil or the key
// is absent, it returns an *AnswerError with reason missing_answer; if
// resp.Invalid has an error for the key, it returns that error together with
// the answer when the stored answer is an A (so a caller who accepts
// ErrInconsistentAnswer can proceed); if the stored answer is not an A, it
// returns type_mismatch. Other keys of the same response stay usable.
func (h Handle[A]) From(resp *Response) (A, error) {
	return answerAs[A](resp, h.key, h.qtype)
}

// Ask adds q under key and returns a typed handle. It panics if req is nil,
// key is empty, or key is already present: these are programming errors,
// like registering a duplicate route in net/http.
func Ask[A Answer](req *Request, key string, q QuestionFor[A]) Handle[A] {
	if req == nil {
		panic("sod: Ask with nil request")
	}
	if key == "" {
		panic("sod: Ask with empty key")
	}
	if req.Questions == nil {
		req.Questions = make(map[string]Question)
	}
	if _, ok := req.Questions[key]; ok {
		panic(fmt.Sprintf("sod: Ask: question %q already present", key))
	}
	req.Questions[key] = q
	return Handle[A]{key: key, qtype: q.QuestionType()}
}

// AnswerAs is the non-handle form of Handle.From, with the same rules: a
// zero A only for missing_answer and type_mismatch; for a key listed in
// resp.Invalid it returns the answer and that error together, so a caller
// who accepts ErrInconsistentAnswer can still use the answer.
func AnswerAs[A Answer](resp *Response, key string) (A, error) {
	return answerAs[A](resp, key, "")
}

func answerAs[A Answer](resp *Response, key, qtype string) (A, error) {
	var zero A
	if resp == nil {
		return zero, &AnswerError{Key: key, Type: qtype, Reason: ReasonMissingAnswer, Detail: "response is nil"}
	}
	got, ok := resp.Answers[key]
	if !ok || got == nil {
		return zero, &AnswerError{Key: key, Type: qtype, Reason: ReasonMissingAnswer, Detail: "no answer for key"}
	}
	a, ok := got.(A)
	if ae := resp.Invalid[key]; ae != nil {
		if ok {
			return a, ae
		}
		return zero, ae
	}
	if !ok {
		if qtype == "" {
			qtype = got.AnswerType()
		}
		return zero, &AnswerError{Key: key, Type: qtype, Reason: ReasonTypeMismatch,
			Detail: fmt.Sprintf("answer is %T, want %T", got, zero)}
	}
	return a, nil
}
