package gate

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"

	"github.com/deepnoodle-ai/decide"
)

// Kind says what an Input holds.
type Kind uint8

// Input kinds.
const (
	KindMissing   Kind = iota // no answer under this name (the zero value)
	KindFailed                // answer present but unusable; Err says why
	KindAbstained             // an abstention from patterns/pick, reported by the caller
	KindNoul
	KindChoice
	KindScore
)

// String returns the lowercase kind name, else "Kind(n)".
func (k Kind) String() string {
	switch k {
	case KindMissing:
		return "missing"
	case KindFailed:
		return "failed"
	case KindAbstained:
		return "abstained"
	case KindNoul:
		return "noul"
	case KindChoice:
		return "choice"
	case KindScore:
		return "score"
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Input is the narrow view rules read. Fields are exported so gateway
// dialects and hand-computed values can build one directly.
type Input struct {
	Name          string
	Kind          Kind
	Noul          float64            // KindNoul: P(true) as received
	Probabilities map[string]float64 // KindChoice, KindScore; score keys "0".."n-1"
	Choice        string             // KindChoice: the chosen key
	Confidence    float64            // the API's reported confidence
	HasConfidence bool               // false for Noul and for dialects that omit it
	Err           error              // KindFailed
}

// Noul adapts a noul answer. A nil a gives Missing(name).
//
// A NoulAnswer's Noul == 0 is a real reading: the client's missing_field
// check guarantees the field was present.
func Noul(name string, a *decide.NoulAnswer) Input {
	if a == nil {
		return Missing(name)
	}
	return Input{Name: name, Kind: KindNoul, Noul: a.Noul}
}

// Choice adapts a choice answer, copying its probabilities. A nil a gives
// Missing(name).
func Choice(name string, a *decide.ChoiceAnswer) Input {
	if a == nil {
		return Missing(name)
	}
	return Input{Name: name, Kind: KindChoice, Probabilities: maps.Clone(a.Probabilities),
		Choice: a.Choice, Confidence: a.Confidence, HasConfidence: true}
}

// Score adapts a score answer, copying its probabilities. A nil a gives
// Missing(name).
func Score(name string, a *decide.ScoreAnswer) Input {
	if a == nil {
		return Missing(name)
	}
	return Input{Name: name, Kind: KindScore, Probabilities: maps.Clone(a.Probabilities),
		Confidence: a.Confidence, HasConfidence: true}
}

// Missing returns an input with no answer.
func Missing(name string) Input { return Input{Name: name, Kind: KindMissing} }

// Failed returns an input whose answer is present but unusable.
func Failed(name string, err error) Input { return Input{Name: name, Kind: KindFailed, Err: err} }

// Abstained returns an input for an abstention from patterns/pick, so that rules route it
// to OnMissing.
func Abstained(name string) Input { return Input{Name: name, Kind: KindAbstained} }

// Inputs is an immutable set of inputs keyed by Name. The zero value is an
// empty set.
type Inputs struct{ m map[string]Input }

// NewInputs returns a set of inputs. It panics on an empty or duplicate
// name, as decide.Ask does: these are programming errors.
func NewInputs(in ...Input) Inputs {
	m := make(map[string]Input, len(in))
	for _, x := range in {
		if x.Name == "" {
			panic("gate: NewInputs with empty name")
		}
		if _, ok := m[x.Name]; ok {
			panic(fmt.Sprintf("gate: NewInputs: duplicate name %q", x.Name))
		}
		x.Probabilities = maps.Clone(x.Probabilities)
		m[x.Name] = x
	}
	return Inputs{m: m}
}

// Get returns the input under name, or Missing(name) if there is none.
func (s Inputs) Get(name string) Input {
	if x, ok := s.m[name]; ok {
		x.Probabilities = maps.Clone(x.Probabilities)
		return x
	}
	return Missing(name)
}

// All yields every input, sorted by name.
func (s Inputs) All() iter.Seq2[string, Input] {
	return func(yield func(string, Input) bool) {
		for _, k := range slices.Sorted(maps.Keys(s.m)) {
			if !yield(k, s.Get(k)) {
				return
			}
		}
	}
}

// AdaptOption configures FromNoul, FromChoice, FromScore, and FromResponse.
type AdaptOption func(*adaptConfig)

type adaptConfig struct{ acceptInconsistent bool }

func newAdaptConfig(opts []AdaptOption) adaptConfig {
	var c adaptConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// AcceptInconsistent reads an answer whose error matches
// decide.ErrInconsistentAnswer (the choice is not the argmax, the
// probabilities do not sum to about 1, or a score is out of range) as normal
// input. It needs the answer itself (a non-nil a, or resp.Answers[key]);
// without it the input stays Failed. The default is Failed, because an
// inconsistent distribution makes every certainty measure questionable.
func AcceptInconsistent() AdaptOption {
	return func(c *adaptConfig) { c.acceptInconsistent = true }
}

func (c adaptConfig) accepts(err error) bool {
	return c.acceptInconsistent && errors.Is(err, decide.ErrInconsistentAnswer)
}

// FromNoul adapts the result of a noul handle's From:
//
//	a, err := pathOK.From(resp)
//	in := gate.FromNoul("path_ok", a, err)
//
// A non-nil err gives Failed(name, err), except as AcceptInconsistent says.
func FromNoul(name string, a *decide.NoulAnswer, err error, opts ...AdaptOption) Input {
	if err != nil && (a == nil || !newAdaptConfig(opts).accepts(err)) {
		return Failed(name, err)
	}
	return Noul(name, a)
}

// FromChoice is FromNoul for a choice answer.
func FromChoice(name string, a *decide.ChoiceAnswer, err error, opts ...AdaptOption) Input {
	if err != nil && (a == nil || !newAdaptConfig(opts).accepts(err)) {
		return Failed(name, err)
	}
	return Choice(name, a)
}

// FromScore is FromNoul for a score answer.
func FromScore(name string, a *decide.ScoreAnswer, err error, opts ...AdaptOption) Input {
	if err != nil && (a == nil || !newAdaptConfig(opts).accepts(err)) {
		return Failed(name, err)
	}
	return Score(name, a)
}

// FromResponse adapts every answer in resp.Answers. A *decide.RawAnswer or
// an answer type this package does not know becomes Failed. Each key in
// resp.Invalid becomes Failed with its *decide.AnswerError, except as
// AcceptInconsistent says. Pass resp even when SystemOne also returned an
// error; a nil resp gives an empty Inputs, so every rule sees Missing.
func FromResponse(resp *decide.Response, opts ...AdaptOption) Inputs {
	if resp == nil {
		return Inputs{}
	}
	cfg := newAdaptConfig(opts)
	m := make(map[string]Input, len(resp.Answers)+len(resp.Invalid))
	for key, a := range resp.Answers {
		if ae := resp.Invalid[key]; ae != nil && !cfg.accepts(ae) {
			m[key] = Failed(key, ae)
			continue
		}
		m[key] = adapt(key, a)
	}
	for key, ae := range resp.Invalid {
		if _, ok := m[key]; !ok && ae != nil {
			m[key] = Failed(key, ae)
		}
	}
	return Inputs{m: m}
}

func adapt(key string, a decide.Answer) Input {
	switch a := a.(type) {
	case nil:
		return Missing(key)
	case *decide.NoulAnswer:
		return Noul(key, a)
	case *decide.ChoiceAnswer:
		return Choice(key, a)
	case *decide.ScoreAnswer:
		return Score(key, a)
	case *decide.RawAnswer:
		if a == nil {
			return Missing(key)
		}
		if a.Err != nil {
			return Failed(key, a.Err)
		}
		return Failed(key, fmt.Errorf("gate: answer type %q is not supported", a.Type))
	default:
		return Failed(key, fmt.Errorf("gate: answer type %T is not supported", a))
	}
}
