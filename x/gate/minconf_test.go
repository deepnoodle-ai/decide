package gate_test

import (
	"math/rand/v2"
	"testing"

	"github.com/deepnoodle-ai/decide/x/gate"
)

func TestMinConfidence(t *testing.T) {
	// Gate a click on the irreversible answers only. 0.4 is a placeholder.
	r := gate.MinConfidence{Inputs: []string{"kind", "item"}, Measure: gate.MeasureConfidence, Floor: 0.4}
	in := gate.NewInputs(conf("kind", 0.7), conf("item", 0.5), conf("scroll", 0.05))
	d := r.Evaluate(in)
	check(t, d, gate.Allow, "item", false)
	if d.Reason != "min confidence=0.5 at or above floor 0.4" || len(d.Readings) != 2 {
		t.Fatalf("%q %+v", d.Reason, d.Readings)
	}

	low := gate.NewInputs(conf("kind", 0.7), conf("item", 0.3))
	d = r.Evaluate(low)
	check(t, d, gate.Escalate, "item", false)
	if d.Reason != "item confidence=0.3 below floor 0.4 -> escalate" {
		t.Fatalf("%q", d.Reason)
	}
	r.Below = gate.Review
	check(t, r.Evaluate(low), gate.Review, "item", false)

	// At the floor allows; just below does not.
	check(t, r.Evaluate(gate.NewInputs(conf("kind", 0.4), conf("item", 0.9))), gate.Allow, "kind", false)
	check(t, r.Evaluate(gate.NewInputs(conf("kind", down(0.4)), conf("item", 0.9))), gate.Review, "kind", false)

	// Missing uses OnMissing, not Below.
	check(t, r.Evaluate(gate.NewInputs(conf("kind", 0.9))), gate.Escalate, "item", true)

	// Applicability on a Noul.
	app := gate.MinConfidence{Inputs: []string{"applies"}, Measure: gate.MeasureApplicability, Floor: 0.8}
	check(t, app.Evaluate(gate.NewInputs(noul("applies", 0.05))), gate.Allow, "applies", false)
	check(t, app.Evaluate(gate.NewInputs(noul("applies", 0.5))), gate.Escalate, "applies", false)
}

// MinConfidence equals an AllOf with HighIsSafe, Allow == Review == Floor,
// and Below == Escalate.
func TestMinConfidenceEqualsAllOf(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	names := []string{"a", "b", "c", "d"}
	measures := []gate.Measure{gate.MeasureConfidence, gate.MeasureTop, gate.MeasureMargin,
		gate.MeasureEntropy, gate.MeasureApplicability}
	onMissing := []gate.Outcome{0, gate.Allow, gate.Review, gate.Escalate}
	for i := range 1000 {
		var ins []gate.Input
		for _, n := range names {
			switch rng.IntN(6) {
			case 0:
				continue // missing
			case 1:
				ins = append(ins, noul(n, rng.Float64()))
			case 2:
				ins = append(ins, gate.Failed(n, errString("x")))
			default:
				k := 1 + rng.IntN(4)
				probs := make(map[string]float64, k)
				sum := 0.0
				for j := range k {
					p := rng.Float64()
					probs[string(rune('p'+j))] = p
					sum += p
				}
				for key := range probs {
					probs[key] /= sum
				}
				ins = append(ins, choice(n, rng.Float64(), probs))
			}
		}
		subset := names[:1+rng.IntN(len(names))]
		floor := rng.Float64()
		m := measures[rng.IntN(len(measures))]
		om := onMissing[rng.IntN(len(onMissing))]
		in := gate.NewInputs(ins...)
		mc := gate.MinConfidence{Inputs: subset, Measure: m, Floor: floor, Below: gate.Escalate, OnMissing: om}
		ao := gate.AllOf{Inputs: subset, Measure: m, Polarity: gate.HighIsSafe, Allow: floor, Review: floor, OnMissing: om}
		a, b := mc.Evaluate(in), ao.Evaluate(in)
		if a.Outcome != b.Outcome || a.Weakest != b.Weakest || a.Value != b.Value || a.Missing != b.Missing {
			t.Fatalf("case %d: min_confidence %s/%s/%v/%v, all_of %s/%s/%v/%v (%s | %s)", i,
				a.Outcome, a.Weakest, a.Value, a.Missing, b.Outcome, b.Weakest, b.Value, b.Missing, a.Reason, b.Reason)
		}
	}
}
