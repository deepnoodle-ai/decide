package gate

import (
	"maps"
	"math"
	"slices"
	"strconv"
)

// Measure names the number a rule reads from an input. There is no default:
// the empty measure is invalid.
type Measure string

// Measures. The first five are certainty measures; noul and prob are raw
// values for Bands and AllOf.
const (
	MeasureConfidence    Measure = "confidence"    // the API's reported confidence
	MeasureTop           Measure = "top"           // max p
	MeasureMargin        Measure = "margin"        // top minus runner-up
	MeasureEntropy       Measure = "entropy"       // 1 - H(p)/ln k
	MeasureApplicability Measure = "applicability" // 0.5 + |p - 0.5|, Noul only
	MeasureNoul          Measure = "noul"          // raw P(true), Noul only
	MeasureProb          Measure = "prob"          // P(option)
)

func (m Measure) known() bool {
	switch m {
	case MeasureConfidence, MeasureTop, MeasureMargin, MeasureEntropy,
		MeasureApplicability, MeasureNoul, MeasureProb:
		return true
	}
	return false
}

func (m Measure) certainty() bool {
	switch m {
	case MeasureConfidence, MeasureTop, MeasureMargin, MeasureEntropy, MeasureApplicability:
		return true
	}
	return false
}

// tol matches the client's tolerance at the [0,1] bounds.
const tol = 1e-6

func badUnit(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0) || f < -tol || f > 1+tol
}

// Measure computes measure m of in. option is used only by MeasureProb.
//
// For a Noul with P(true) = p, top, margin, entropy, and prob read the
// two-point distribution {"true": p, "false": 1-p}. Nothing is
// renormalized; entropy is clamped to [0,1], since a sum near 1 can push it
// just outside.
//
// Errors wrap ErrMissing (missing, failed, or abstained input), ErrUndefined
// (unknown measure, or not defined for the input's kind), or ErrBadValue (a
// value read is NaN, infinite, empty, or outside [-1e-6, 1+1e-6]).
func (in Input) Measure(m Measure, option string) (float64, error) {
	switch in.Kind {
	case KindMissing:
		return 0, &readError{problem: problemMissing, reason: in.Name + " missing", sentinel: ErrMissing}
	case KindFailed:
		reason := in.Name + " failed"
		if in.Err != nil {
			reason += ": " + in.Err.Error()
		}
		return 0, &readError{problem: problemFailed, reason: reason, sentinel: ErrMissing, err: in.Err}
	case KindAbstained:
		return 0, &readError{problem: problemAbstained, reason: in.Name + " abstained", sentinel: ErrMissing}
	case KindNoul:
		return in.noulMeasure(m, option)
	case KindChoice, KindScore:
		return in.distMeasure(m, option)
	}
	return 0, in.undefined(m)
}

func (in Input) undefined(m Measure) error {
	return &readError{problem: problemUndefined, sentinel: ErrUndefined,
		reason: in.Name + " " + string(m) + " undefined for " + in.Kind.String()}
}

func (in Input) badValue(m Measure) error {
	return &readError{problem: problemBadValue, sentinel: ErrBadValue,
		reason: in.Name + " " + string(m) + " bad value"}
}

func (in Input) noulMeasure(m Measure, option string) (float64, error) {
	if !m.known() || m == MeasureConfidence {
		return 0, in.undefined(m)
	}
	if m == MeasureProb && option != "true" && option != "false" {
		return 0, in.undefined(m)
	}
	p := in.Noul
	if badUnit(p) {
		return 0, in.badValue(m)
	}
	switch m {
	case MeasureTop:
		return max(p, 1-p), nil
	case MeasureMargin:
		return math.Abs(2*p - 1), nil
	case MeasureEntropy:
		return normEntropy([]float64{p, 1 - p}), nil
	case MeasureApplicability:
		return 0.5 + math.Abs(p-0.5), nil
	case MeasureNoul:
		return p, nil
	case MeasureProb:
		if option == "true" {
			return p, nil
		}
		return 1 - p, nil
	}
	return 0, in.undefined(m)
}

func (in Input) distMeasure(m Measure, option string) (float64, error) {
	switch m {
	case MeasureConfidence:
		if !in.HasConfidence {
			return 0, in.undefined(m)
		}
		if badUnit(in.Confidence) {
			return 0, in.badValue(m)
		}
		return in.Confidence, nil
	case MeasureTop, MeasureMargin, MeasureEntropy, MeasureProb:
	default:
		return 0, in.undefined(m)
	}
	if len(in.Probabilities) == 0 {
		return 0, in.badValue(m)
	}
	// Sorted keys make the entropy sum bit-for-bit deterministic; map
	// order would change the last digits between runs.
	ps := make([]float64, 0, len(in.Probabilities))
	for _, k := range slices.Sorted(maps.Keys(in.Probabilities)) {
		p := in.Probabilities[k]
		if badUnit(p) {
			return 0, in.badValue(m)
		}
		ps = append(ps, p)
	}
	switch m {
	case MeasureTop:
		t, _ := top2(ps)
		return t, nil
	case MeasureMargin:
		if len(ps) == 1 {
			return ps[0], nil
		}
		t, r := top2(ps)
		return t - r, nil
	case MeasureEntropy:
		return normEntropy(ps), nil
	}
	p, ok := in.Probabilities[option] // MeasureProb
	if !ok {
		return 0, in.undefined(m)
	}
	return p, nil
}

// top2 returns the largest and second-largest values; ps is non-empty.
func top2(ps []float64) (first, second float64) {
	first, second = math.Inf(-1), math.Inf(-1)
	for _, p := range ps {
		if p > first {
			first, second = p, first
		} else if p > second {
			second = p
		}
	}
	return first, second
}

// normEntropy is 1 - H/ln k with 0 ln 0 = 0, 1 when k = 1, clamped to [0,1].
func normEntropy(ps []float64) float64 {
	if len(ps) <= 1 {
		return 1
	}
	h := 0.0
	for _, p := range ps {
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return min(1, max(0, 1-h/math.Log(float64(len(ps)))))
}

// num formats a number as it is compared.
func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
