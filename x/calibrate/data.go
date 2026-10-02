package calibrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"maps"
	"math"
	"slices"
	"strconv"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/gate"
)

var (
	ErrInvalidDataset  = errors.New("calibrate: invalid dataset")
	ErrInvalidConfig   = errors.New("calibrate: invalid config")
	ErrCohortMismatch  = errors.New("calibrate: cohort mismatch")
	ErrNoFit           = errors.New("calibrate: no qualifying cutoff")
	ErrRegression      = errors.New("calibrate: tolerance exceeded")
	ErrUndefinedMetric = errors.New("calibrate: metric unavailable")
)

// Case is one saved state, answer, and ground-truth label. State and Answer
// are JSON from a completed request; they are never sent by this package.
type Case struct {
	ID     string          `json:"id"`
	State  json.RawMessage `json:"state"`
	Answer json.RawMessage `json:"answer"`
	Label  string          `json:"label"`
}

// Dataset holds answers for one saved question and resolved model version.
type Dataset struct {
	QuestionKey string          `json:"question_key"`
	Question    json.RawMessage `json:"question"`
	Model       string          `json:"model"`
	Cases       []Case          `json:"cases"`
}

type sample struct {
	id        string
	state     []byte
	label     string
	input     gate.Input
	predicted string
	probs     map[string]float64
}

type checked struct {
	kind           gate.Kind
	questionDigest string
	cohortDigest   string
	model          string
	samples        []sample
}

// The root validator accepts wider, option-count-dependent sums while its
// live tolerance is provisional. Calibration needs a positive distribution
// near unit mass before entropy, temperature, and scoring are defined.
const distributionSumTolerance = 0.01

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDataset, fmt.Sprintf(format, args...))
}

func compactJSON(raw []byte) ([]byte, error) {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func hashFields(fields ...[]byte) string {
	h := sha256.New()
	for _, b := range fields {
		writeField(h, b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeField(h hash.Hash, b []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(b)))
	h.Write(length[:])
	h.Write(b)
}

func checkDataset(d Dataset) (checked, error) {
	if d.QuestionKey == "" || d.Model == "" || len(d.Cases) == 0 {
		return checked{}, invalid("question key, resolved model, and cases are required")
	}
	questionJSON, err := compactJSON(d.Question)
	if err != nil || bytes.Equal(questionJSON, []byte("null")) {
		return checked{}, invalid("question is not a JSON object")
	}
	q, err := decide.DecodeQuestion(questionJSON)
	if err != nil {
		return checked{}, invalid("question: %v", err)
	}
	var kind gate.Kind
	switch q.(type) {
	case *decide.NoulQuestion:
		kind = gate.KindNoul
	case *decide.ChoiceQuestion:
		kind = gate.KindChoice
	case *decide.ScoreQuestion:
		kind = gate.KindScore
	default:
		return checked{}, invalid("unsupported question type %q", q.QuestionType())
	}
	if err := (&decide.Request{
		State: "validation state", Model: d.Model,
		Questions: map[string]decide.Question{d.QuestionKey: q},
	}).Validate(); err != nil {
		return checked{}, invalid("question: %v", err)
	}
	validator := q.(decide.AnswerValidator)
	out := checked{
		kind: kind, model: d.Model,
		questionDigest: hashFields([]byte(d.QuestionKey), questionJSON),
		samples:        make([]sample, 0, len(d.Cases)),
	}
	seen := make(map[string]bool, len(d.Cases))
	for _, c := range d.Cases {
		if c.ID == "" || seen[c.ID] {
			return checked{}, invalid("case %q: empty or duplicate ID", c.ID)
		}
		seen[c.ID] = true
		stateJSON, err := compactJSON(c.State)
		if err != nil || bytes.Equal(stateJSON, []byte("null")) {
			return checked{}, invalid("case %q: invalid state", c.ID)
		}
		var state any
		if err := json.Unmarshal(stateJSON, &state); err != nil {
			return checked{}, invalid("case %q: state: %v", c.ID, err)
		}
		switch state.(type) {
		case string, map[string]any, []any:
		default:
			return checked{}, invalid("case %q: state must be string, object, or array", c.ID)
		}
		answer, err := decide.DecodeAnswer(c.Answer)
		if err != nil {
			return checked{}, invalid("case %q: answer: %v", c.ID, err)
		}
		if err := validator.ValidateAnswer(answer); err != nil {
			return checked{}, invalid("case %q: answer: %v", c.ID, err)
		}
		s := sample{id: c.ID, state: stateJSON, label: c.Label}
		switch a := answer.(type) {
		case *decide.NoulAnswer:
			if c.Label != "true" && c.Label != "false" {
				return checked{}, invalid("case %q: Noul label must be true or false", c.ID)
			}
			s.input = gate.Noul(d.QuestionKey, a)
			s.probs = map[string]float64{"true": a.Noul, "false": 1 - a.Noul}
		case *decide.ChoiceAnswer:
			if _, ok := a.Probabilities[c.Label]; !ok {
				return checked{}, invalid("case %q: Choice label %q is not an option", c.ID, c.Label)
			}
			s.input = gate.Choice(d.QuestionKey, a)
			s.predicted = a.Choice
			s.probs = a.Probabilities
		case *decide.ScoreAnswer:
			if i, err := strconv.Atoi(c.Label); err != nil || strconv.Itoa(i) != c.Label {
				return checked{}, invalid("case %q: Score label %q is not a level index", c.ID, c.Label)
			}
			if _, ok := a.Probabilities[c.Label]; !ok {
				return checked{}, invalid("case %q: Score label %q is not a level", c.ID, c.Label)
			}
			s.input = gate.Score(d.QuestionKey, a)
			s.probs = a.Probabilities
			for i := range len(a.Probabilities) {
				key := strconv.Itoa(i)
				if s.predicted == "" || a.Probabilities[key] > a.Probabilities[s.predicted] {
					s.predicted = key
				}
			}
		}
		mass := 0.0
		for _, key := range slices.Sorted(maps.Keys(s.probs)) {
			p := s.probs[key]
			if !validUnit(p) {
				return checked{}, invalid("case %q: probability %q outside [0,1]", c.ID, key)
			}
			mass += p
		}
		if mass <= 0 || math.Abs(mass-1) > distributionSumTolerance {
			return checked{}, invalid(
				"case %q: probabilities sum to %g, want positive mass within %g of 1",
				c.ID, mass, distributionSumTolerance,
			)
		}
		out.samples = append(out.samples, s)
	}
	slices.SortFunc(out.samples, func(a, b sample) int {
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	h := sha256.New()
	for _, s := range out.samples {
		writeField(h, []byte(s.id))
		writeField(h, s.state)
		writeField(h, []byte(s.label))
	}
	out.cohortDigest = hex.EncodeToString(h.Sum(nil))
	return out, nil
}
