package gate_test

import (
	"testing"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// A cache-hit check: min over five atomic Nouls, failing closed. The 0.8 bound
// is a placeholder.
var fiveNouls = gate.AllOf{Name: "cache_hit", Inputs: []string{"q1", "q2", "q3", "q4", "q5"},
	Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.8}

func TestAllOf(t *testing.T) {
	all := func(ps ...float64) gate.Inputs {
		var in []gate.Input
		for i, p := range ps {
			in = append(in, noul("q"+string(rune('1'+i)), p))
		}
		return gate.NewInputs(in...)
	}
	t.Run("all high", func(t *testing.T) {
		d := fiveNouls.Evaluate(all(0.9, 0.95, 0.85, 0.99, 0.9))
		check(t, d, gate.Allow, "q3", false)
		if d.Value != 0.85 || len(d.Readings) != 5 {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("one low", func(t *testing.T) {
		d := fiveNouls.Evaluate(all(0.9, 0.95, 0.3, 0.99, 0.9))
		check(t, d, gate.Escalate, "q3", false)
		if want := "q3 noul=0.3 -> escalate (allow 0.8, review 0.8, high_is_safe)"; d.Reason != want {
			t.Fatalf("reason %q", d.Reason)
		}
	})
	t.Run("one failed", func(t *testing.T) {
		in := gate.NewInputs(noul("q1", 0.9), noul("q2", 0.9), gate.Failed("q3", errString("timeout")), noul("q4", 0.9), noul("q5", 0.9))
		d := fiveNouls.Evaluate(in)
		check(t, d, gate.Escalate, "q3", true)
		if d.Readings[2].Problem != "failed" {
			t.Fatalf("%+v", d.Readings)
		}
	})
	t.Run("tie picks earliest", func(t *testing.T) {
		d := fiveNouls.Evaluate(all(0.9, 0.5, 0.9, 0.5, 0.9))
		check(t, d, gate.Escalate, "q2", false)
	})
	t.Run("high is risky takes the max", func(t *testing.T) {
		r := gate.AllOf{Inputs: []string{"a", "b", "c"}, Measure: gate.MeasureNoul, Polarity: gate.HighIsRisky, Allow: 0.2, Review: 0.5}
		d := r.Evaluate(gate.NewInputs(noul("a", 0.1), noul("b", 0.4), noul("c", 0.05)))
		check(t, d, gate.Review, "b", false)
		d = r.Evaluate(gate.NewInputs(noul("a", 0.1), noul("b", 0.1), noul("c", 0.05)))
		check(t, d, gate.Allow, "a", false)
	})
	t.Run("missing reported at equal severity", func(t *testing.T) {
		// q5 is missing (OnMissing unset: Escalate) and q1 is clean but
		// escalates; the missing input is what an operator needs.
		in := gate.NewInputs(noul("q1", 0.1), noul("q2", 0.9), noul("q3", 0.9), noul("q4", 0.9))
		d := fiveNouls.Evaluate(in)
		check(t, d, gate.Escalate, "q5", true)
	})
	t.Run("earliest missing among equals", func(t *testing.T) {
		d := fiveNouls.Evaluate(gate.NewInputs(noul("q1", 0.9), noul("q3", 0.9), noul("q5", 0.9)))
		check(t, d, gate.Escalate, "q2", true)
	})
	t.Run("less severe missing does not override", func(t *testing.T) {
		r := fiveNouls
		r.OnMissing = gate.Review
		d := r.Evaluate(gate.NewInputs(noul("q1", 0.1), noul("q2", 0.9), noul("q3", 0.9), noul("q4", 0.9)))
		check(t, d, gate.Escalate, "q1", false)
	})
	t.Run("more severe missing overrides", func(t *testing.T) {
		r := fiveNouls
		r.OnMissing = gate.Review
		d := r.Evaluate(all(0.9, 0.9, 0.9, 0.9))
		check(t, d, gate.Review, "q5", true)
	})
	t.Run("fail open", func(t *testing.T) {
		r := fiveNouls
		r.OnMissing = gate.Allow
		d := r.Evaluate(all(0.9, 0.9, 0.9, 0.9))
		check(t, d, gate.Allow, "q5", true)
	})
	t.Run("all missing", func(t *testing.T) {
		check(t, fiveNouls.Evaluate(gate.Inputs{}), gate.Escalate, "q1", true)
	})
}
