package gate_test

import (
	"testing"

	"github.com/deepnoodle-ai/decide/x/gate"
)

func TestMargin(t *testing.T) {
	// Auto-accept only when top >= 0.85 and top - runnerUp >= 0.5, else
	// review. The bounds are placeholders.
	topLead := gate.Margin{Input: "x", Top: 0.85, Lead: 0.5, Below: gate.Review}

	// {0.875, 0.125}: top 0.875 and margin 0.75, both exact in binary, so
	// "at", "above", and "below" move the bound by one ulp.
	exact := gate.NewInputs(choice("x", 0.75, map[string]float64{"a": 0.875, "b": 0.125}))
	for _, tc := range []struct {
		name      string
		top, lead float64
		want      gate.Outcome
		value     float64
	}{
		{"top at", 0.875, 0.5, gate.Allow, 0.75},
		{"top value above bound", down(0.875), 0.5, gate.Allow, 0.75},
		{"top value below bound", up(0.875), 0.5, gate.Review, 0.875},
		{"lead at", 0.85, 0.75, gate.Allow, 0.75},
		{"lead value above bound", 0.85, down(0.75), gate.Allow, 0.75},
		{"lead value below bound", 0.85, up(0.75), gate.Review, 0.75},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := topLead
			r.Top, r.Lead = tc.top, tc.lead
			d := r.Evaluate(exact)
			check(t, d, tc.want, "x", false)
			if d.Value != tc.value || len(d.Readings) != 2 ||
				d.Readings[0].Measure != gate.MeasureTop || d.Readings[1].Measure != gate.MeasureMargin {
				t.Fatalf("value %v readings %+v", d.Value, d.Readings)
			}
		})
	}

	for _, tc := range []struct {
		name string
		in   gate.Input
		want gate.Outcome
	}{
		{"clear winner", choice("x", 0.85, map[string]float64{"a": 0.9, "b": 0.05, "c": 0.05}), gate.Allow},
		{"top too low", choice("x", 0.7, map[string]float64{"a": 0.8, "b": 0.1, "c": 0.1}), gate.Review},
		{"noul confident yes", noul("x", 0.9), gate.Allow},
		{"noul confident no", noul("x", 0.05), gate.Allow},
		{"noul unsure", noul("x", 0.3), gate.Review},
		{"one key certain", choice("x", 1, map[string]float64{"a": 1}), gate.Allow},
		{"one key partial", choice("x", 1, map[string]float64{"a": 0.8}), gate.Review},
		// Lead fails although top passes: only possible when the
		// probabilities do not sum to 1.
		{"lead fails", gate.Input{Name: "x", Kind: gate.KindChoice, Probabilities: map[string]float64{"a": 0.9, "b": 0.5}}, gate.Review},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check(t, topLead.Evaluate(gate.NewInputs(tc.in)), tc.want, "x", false)
		})
	}

	d := topLead.Evaluate(gate.NewInputs(choice("x", 0.7, map[string]float64{"a": 0.8, "b": 0.1, "c": 0.1})))
	if want := "x top=0.8 (need 0.85) margin=0.7000000000000001 (need 0.5) -> review"; d.Reason != want {
		t.Errorf("reason %q", d.Reason)
	}

	t.Run("missing", func(t *testing.T) {
		d := topLead.Evaluate(gate.Inputs{})
		check(t, d, gate.Escalate, "x", true)
		if d.Readings[0].Problem != "missing" || d.Readings[1].Problem != "missing" {
			t.Fatalf("%+v", d.Readings)
		}
		r := topLead
		r.OnMissing = gate.Allow
		check(t, r.Evaluate(gate.NewInputs(gate.Abstained("x"))), gate.Allow, "x", true)
	})
	t.Run("below unset escalates", func(t *testing.T) {
		r := topLead
		r.Below = 0
		check(t, r.Evaluate(gate.NewInputs(noul("x", 0.3))), gate.Escalate, "x", false)
	})
}
