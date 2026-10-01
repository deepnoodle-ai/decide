package decide

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
)

// Answer is one entry in Response.Answers. Implementations outside this
// package are allowed.
type Answer interface {
	AnswerType() string
}

// NoulAnswer is the answer to a NoulQuestion. Noul is P(yes) in [0,1]. There
// is no confidence field; the API does not return one.
type NoulAnswer struct {
	Noul  float64
	Extra map[string]json.RawMessage // unmodeled members, as received

	missing []string // required members absent or null when decoded
}

// ChoiceAnswer is the answer to a ChoiceQuestion.
type ChoiceAnswer struct {
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
	Extra         map[string]json.RawMessage

	missing []string
}

// ScoreAnswer is the answer to a ScoreQuestion. Score is the
// probability-weighted level, in [0, levels-1].
//
// Discrepancy: the docs say legend values are strings; the OpenAPI spec
// allows string, object, or array. Legend therefore holds any.
type ScoreAnswer struct {
	Score         float64
	Legend        map[string]any     // keys "0".."n-1"
	Probabilities map[string]float64 // keys "0".."n-1"
	Confidence    float64
	Extra         map[string]json.RawMessage

	missing []string
}

// RawAnswer holds an answer whose type is not registered, or a registered
// type that failed to decode (Err is then non-nil). JSON is the full answer
// object as received. AnswerType returns Type.
type RawAnswer struct {
	Type string
	JSON json.RawMessage
	Err  error
}

// AnswerType returns "noul".
func (a *NoulAnswer) AnswerType() string { return typeNoul }

// AnswerType returns "choice".
func (a *ChoiceAnswer) AnswerType() string { return typeChoice }

// AnswerType returns "score".
func (a *ScoreAnswer) AnswerType() string { return typeScore }

// AnswerType returns a.Type.
func (a *RawAnswer) AnswerType() string { return a.Type }

// MarshalJSON writes "type" first, then the fields, then Extra sorted by key.
func (a *NoulAnswer) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("type", typeNoul)
	w.field("noul", a.Noul)
	w.rawExtra(a.Extra, "type", "noul")
	return w.bytes()
}

// MarshalJSON writes "type" first, then the fields, then Extra sorted by key.
// Probabilities are written with keys sorted, not in wire order (Go maps are
// unordered), so a round trip is equal semantically, not byte for byte.
func (a *ChoiceAnswer) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("type", typeChoice)
	w.field("choice", a.Choice)
	w.field("probabilities", a.Probabilities)
	w.field("confidence", a.Confidence)
	w.rawExtra(a.Extra, "type", "choice", "probabilities", "confidence")
	return w.bytes()
}

// MarshalJSON writes "type" first, then the fields, then Extra sorted by key.
// Legend and probabilities are written with keys sorted, not in wire order.
func (a *ScoreAnswer) MarshalJSON() ([]byte, error) {
	w := newObjectWriter()
	w.field("type", typeScore)
	w.field("score", a.Score)
	w.field("legend", a.Legend)
	w.field("probabilities", a.Probabilities)
	w.field("confidence", a.Confidence)
	w.rawExtra(a.Extra, "type", "score", "legend", "probabilities", "confidence")
	return w.bytes()
}

// MarshalJSON returns JSON as received, or {"type": Type} if JSON is empty.
func (a *RawAnswer) MarshalJSON() ([]byte, error) {
	if len(a.JSON) == 0 {
		w := newObjectWriter()
		w.field("type", a.Type)
		return w.bytes()
	}
	return a.JSON, nil
}

// The built-in answers track which required members were absent or null.
// encoding/json would leave them at zero, which reads as a confident "no";
// validation reports them as missing_field instead. A member of the wrong
// JSON type is a decode error.

// UnmarshalJSON decodes a noul answer. "noul" is required.
func (a *NoulAnswer) UnmarshalJSON(data []byte) error {
	var v NoulAnswer
	missing, extra, err := decodeFields(data, map[string]any{"noul": &v.Noul})
	if err != nil {
		return fmt.Errorf("decide: noul answer: %w", err)
	}
	v.Extra, v.missing = extra, missing
	*a = v
	return nil
}

// UnmarshalJSON decodes a choice answer. All three fields are required.
func (a *ChoiceAnswer) UnmarshalJSON(data []byte) error {
	var v ChoiceAnswer
	missing, extra, err := decodeFields(data, map[string]any{
		"choice":        &v.Choice,
		"probabilities": &v.Probabilities,
		"confidence":    &v.Confidence,
	})
	if err != nil {
		return fmt.Errorf("decide: choice answer: %w", err)
	}
	v.Extra, v.missing = extra, missing
	*a = v
	return nil
}

// UnmarshalJSON decodes a score answer. All four fields are required.
func (a *ScoreAnswer) UnmarshalJSON(data []byte) error {
	var v ScoreAnswer
	missing, extra, err := decodeFields(data, map[string]any{
		"score":         &v.Score,
		"legend":        &v.Legend,
		"probabilities": &v.Probabilities,
		"confidence":    &v.Confidence,
	})
	if err != nil {
		return fmt.Errorf("decide: score answer: %w", err)
	}
	v.Extra, v.missing = extra, missing
	*a = v
	return nil
}

// UnmarshalJSON keeps a copy of data and reads Type from it.
func (a *RawAnswer) UnmarshalJSON(data []byte) error {
	typ, _, err := readType(data)
	a.Type, a.JSON, a.Err = typ, bytes.Clone(data), nil
	return err
}

// decodeFields decodes the required members named in fields into their
// targets. It returns the names that were absent or null, sorted, and the
// members other than "type" and the required ones.
func decodeFields(data []byte, fields map[string]any) (missing []string, extra map[string]json.RawMessage, err error) {
	m, err := decodeObject(data)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		raw, ok := m[name]
		if !ok || isNull(raw) {
			missing = append(missing, name)
			continue
		}
		if err := json.Unmarshal(raw, fields[name]); err != nil {
			return nil, nil, fmt.Errorf("%q: %w", name, err)
		}
	}
	for k, v := range m {
		if _, known := fields[k]; known || k == "type" {
			continue
		}
		if extra == nil {
			extra = make(map[string]json.RawMessage)
		}
		extra[k] = v
	}
	return missing, extra, nil
}

// Registry.

var (
	registryMu sync.RWMutex
	registry   = map[string]func() Answer{
		typeNoul:   func() Answer { return &NoulAnswer{} },
		typeChoice: func() Answer { return &ChoiceAnswer{} },
		typeScore:  func() Answer { return &ScoreAnswer{} },
	}
)

// RegisterAnswerType registers a constructor for an answer type, used when
// a response is decoded without its request (or the request's question does
// not implement AnswerMaker). It returns an error if typ is empty, newAnswer is
// nil, or typ is already registered, including the built-in types.
//
// The constructor must return a pointer that encoding/json can unmarshal
// into.
func RegisterAnswerType(typ string, newAnswer func() Answer) error {
	if typ == "" {
		return errors.New("decide: RegisterAnswerType with empty type")
	}
	if newAnswer == nil {
		return errors.New("decide: RegisterAnswerType with nil constructor")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[typ]; ok {
		return fmt.Errorf("decide: answer type %q already registered", typ)
	}
	registry[typ] = newAnswer
	return nil
}

func registered(typ string) func() Answer {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[typ]
}

// DecodeAnswer reads "type", constructs the registered answer, and
// unmarshals into it. An unregistered type returns a *RawAnswer and nil
// error. A registered type that fails to unmarshal returns a *RawAnswer with
// Err set, and the same error. Input that is not an object with a string
// "type" returns a *RawAnswer with Type "" and Err set.
func DecodeAnswer(data []byte) (Answer, error) {
	return decodeAnswer(data, nil)
}

// decodeAnswer is DecodeAnswer with an optional question. When q's type
// matches the answer's, q's NewAnswer is tried before the registry.
func decodeAnswer(data []byte, q Question) (Answer, error) {
	raw := &RawAnswer{JSON: bytes.Clone(data)}
	typ, _, err := readType(data)
	if err != nil {
		raw.Err = fmt.Errorf("decide: decode answer: %w", err)
		return raw, raw.Err
	}
	raw.Type = typ
	var a Answer
	if q != nil && q.QuestionType() == typ {
		a = newAnswerFor(q)
	}
	if a == nil {
		if newAnswer := registered(typ); newAnswer != nil {
			a = newAnswer()
		}
	}
	if a == nil {
		return raw, nil
	}
	if err := json.Unmarshal(data, a); err != nil {
		raw.Err = fmt.Errorf("decide: decode %s answer: %w", typ, err)
		return raw, raw.Err
	}
	return a, nil
}

// newAnswerFor returns q.MakeAnswer() if q implements AnswerMaker, else nil
// (and a nil MakeAnswer result falls through to the registry). The root
// package uses no reflection: QuestionFor is generic and cannot be
// type-asserted without its type argument, so AnswerMaker exists for this.
func newAnswerFor(q Question) Answer {
	if m, ok := q.(AnswerMaker); ok {
		return m.MakeAnswer()
	}
	return nil
}

// Helpers. These are pure functions of the answer; thresholds and other
// decisions belong to callers.

// Prob is one entry of a probability distribution.
type Prob struct {
	Key string
	P   float64
}

// Ranked returns probabilities sorted by P descending, ties by Key ascending.
func (a *ChoiceAnswer) Ranked() []Prob {
	out := make([]Prob, 0, len(a.Probabilities))
	for k, p := range a.Probabilities {
		out = append(out, Prob{Key: k, P: p})
	}
	slices.SortFunc(out, func(x, y Prob) int {
		if c := cmp.Compare(y.P, x.P); c != 0 {
			return c // P descending
		}
		return cmp.Compare(x.Key, y.Key)
	})
	return out
}

// Margin returns top P minus runner-up P; with one option it returns top P.
// With no probabilities it returns 0.
func (a *ChoiceAnswer) Margin() float64 {
	r := a.Ranked()
	switch len(r) {
	case 0:
		return 0
	case 1:
		return r[0].P
	}
	return r[0].P - r[1].P
}

// Levels returns max(len(Legend), len(Probabilities)). Validation allows a
// legend with missing keys, so the legend alone can undercount.
func (a *ScoreAnswer) Levels() int {
	return max(len(a.Legend), len(a.Probabilities))
}

// ProbabilityAt returns Probabilities[strconv.Itoa(i)].
func (a *ScoreAnswer) ProbabilityAt(i int) (float64, bool) {
	p, ok := a.Probabilities[strconv.Itoa(i)]
	return p, ok
}

// Level returns the index of the highest probability, lowest index on ties;
// -1 if there are no probabilities. Keys that are not integers are ignored.
func (a *ScoreAnswer) Level() int {
	best, bestP := -1, 0.0
	for k, p := range a.Probabilities {
		i, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		if best == -1 || p > bestP || (p == bestP && i < best) {
			best, bestP = i, p
		}
	}
	return best
}

// LevelLabel returns Legend[strconv.Itoa(i)], or nil.
func (a *ScoreAnswer) LevelLabel(i int) any {
	return a.Legend[strconv.Itoa(i)]
}

// Normalized returns Score / (Levels()-1), or 0 when Levels() < 2.
func (a *ScoreAnswer) Normalized() float64 {
	n := a.Levels()
	if n < 2 {
		return 0
	}
	return a.Score / float64(n-1)
}
