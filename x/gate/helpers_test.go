package gate_test

import (
	"errors"
	"math"
	"testing"

	"github.com/deepnoodle-ai/decide/x/gate"
)

// Every threshold in these tests is a placeholder chosen to exercise a
// boundary. None is a recommendation; measure thresholds on your own data.

func noul(name string, p float64) gate.Input {
	return gate.Input{Name: name, Kind: gate.KindNoul, Noul: p}
}

// choice builds a Choice input whose chosen option is the argmax.
func choice(name string, conf float64, probs map[string]float64) gate.Input {
	best, top := "", math.Inf(-1)
	for k, p := range probs {
		if p > top || (p == top && k < best) {
			best, top = k, p
		}
	}
	return gate.Input{Name: name, Kind: gate.KindChoice, Probabilities: probs,
		Choice: best, Confidence: conf, HasConfidence: true}
}

// conf builds a two-option Choice input with the given confidence and a
// matching top probability.
func conf(name string, c float64) gate.Input {
	return choice(name, c, map[string]float64{"a": (1 + c) / 2, "b": (1 - c) / 2})
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func up(x float64) float64   { return math.Nextafter(x, 2) }
func down(x float64) float64 { return math.Nextafter(x, -1) }

func check(t *testing.T, d gate.Decision, outcome gate.Outcome, weakest string, missing bool) {
	t.Helper()
	if d.Outcome != outcome || d.Weakest != weakest || d.Missing != missing {
		t.Fatalf("got %s weakest=%q missing=%v (%s); want %s weakest=%q missing=%v",
			d.Outcome, d.Weakest, d.Missing, d.Reason, outcome, weakest, missing)
	}
	if math.IsNaN(d.Value) {
		t.Fatalf("NaN value")
	}
}

func isInvalidRule(err error) bool { return errors.Is(err, gate.ErrInvalidRule) }
