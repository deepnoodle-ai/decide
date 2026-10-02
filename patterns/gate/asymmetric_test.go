package gate_test

import (
	"testing"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// The voice-banking table from https://docs.typesafe.ai/patterns/confidence-routing.md:
// below 0.6 route to a human; check_balance at 0.6 or more; approve_transfer
// above 0.85 acts, between confirms; any other intent goes to a human. The
// numbers are the docs' placeholders. The docs compare "> 0.85"; bounds here
// are inclusive, so the strict form is up(0.85).
var voiceBanking = gate.Asymmetric{Name: "intent", Input: "intent", Measure: gate.MeasureConfidence, Floor: 0.6,
	Options: map[string]gate.OptionBand{
		"check_balance":    {Allow: 0.6, Review: 0.6},
		"approve_transfer": {Allow: up(0.85), Review: 0.6},
	}}

func intent(chosen string, c float64) gate.Input {
	probs := map[string]float64{"check_balance": 0.1, "approve_transfer": 0.1, "support": 0.1}
	probs[chosen] = 0.8
	in := choice("intent", c, probs)
	return in
}

func TestAsymmetricVoiceBanking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     gate.Input
		want   gate.Outcome
		reason string
	}{
		{"below floor", intent("check_balance", 0.5), gate.Escalate, "intent chose check_balance, confidence=0.5 -> escalate (below floor 0.6)"},
		{"just below floor", intent("approve_transfer", down(0.6)), gate.Escalate, ""},
		{"balance at floor", intent("check_balance", 0.6), gate.Allow, "intent chose check_balance, confidence=0.6 -> allow (allow 0.6)"},
		{"transfer confirm", intent("approve_transfer", 0.7), gate.Review, "intent chose approve_transfer, confidence=0.7 -> review (review 0.6)"},
		{"transfer at 0.85 confirms", intent("approve_transfer", 0.85), gate.Review, ""},
		{"transfer above 0.85 acts", intent("approve_transfer", 0.9), gate.Allow, ""},
		{"other intent", intent("support", 0.95), gate.Escalate, "intent chose support, confidence=0.95 -> escalate (unlisted option)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := voiceBanking.Evaluate(gate.NewInputs(tc.in))
			check(t, d, tc.want, "intent", false)
			if tc.reason != "" && d.Reason != tc.reason {
				t.Fatalf("reason %q, want %q", d.Reason, tc.reason)
			}
		})
	}
}

func TestAsymmetric(t *testing.T) {
	r := voiceBanking
	r.Options = map[string]gate.OptionBand{
		"check_balance":    {Allow: 0.6, Review: 0.6},
		"approve_transfer": {Allow: 0.85, Review: 0.6},
		"support":          {Always: gate.Review},
	}
	d := r.Evaluate(gate.NewInputs(intent("support", 0.99)))
	check(t, d, gate.Review, "intent", false)
	if d.Reason != "intent chose support, confidence=0.99 -> review (always)" {
		t.Errorf("reason %q", d.Reason)
	}
	// The floor is checked before Always.
	check(t, r.Evaluate(gate.NewInputs(intent("support", 0.1))), gate.Escalate, "intent", false)

	// BelowFloor set.
	r.BelowFloor = gate.Review
	check(t, r.Evaluate(gate.NewInputs(intent("check_balance", 0.1))), gate.Review, "intent", false)

	// Others covers unlisted options.
	o := voiceBanking
	o.Others = new(gate.OptionBand{Allow: 0.9, Review: 0.7})
	check(t, o.Evaluate(gate.NewInputs(intent("support", 0.95))), gate.Allow, "intent", false)
	check(t, o.Evaluate(gate.NewInputs(intent("support", 0.8))), gate.Review, "intent", false)
	d = o.Evaluate(gate.NewInputs(intent("support", 0.65)))
	check(t, d, gate.Escalate, "intent", false)
	if d.Reason != "intent chose support, confidence=0.65 -> escalate (below review 0.7)" {
		t.Errorf("reason %q", d.Reason)
	}

	// Floor 0 means none.
	nf := gate.Asymmetric{Input: "intent", Measure: gate.MeasureTop,
		Options: map[string]gate.OptionBand{"check_balance": {Allow: 0.05, Review: 0}}}
	check(t, nf.Evaluate(gate.NewInputs(intent("check_balance", 0.01))), gate.Allow, "intent", false)
	lowTop := choice("intent", 0.01, map[string]float64{"check_balance": 0.34, "support": 0.33, "approve_transfer": 0.33})
	check(t, nf.Evaluate(gate.NewInputs(lowTop)), gate.Allow, "intent", false)
}

func TestAsymmetricMissing(t *testing.T) {
	d := voiceBanking.Evaluate(gate.NewInputs(noul("intent", 0.99)))
	check(t, d, gate.Escalate, "intent", true)
	if d.Reason != "intent confidence undefined for noul -> escalate" || d.Readings[0].Problem != "undefined" {
		t.Errorf("%q %+v", d.Reason, d.Readings)
	}
	// Top is defined for a Noul, but Asymmetric still needs a Choice.
	top := voiceBanking
	top.Measure = gate.MeasureTop
	top.OnMissing = gate.Review
	d = top.Evaluate(gate.NewInputs(noul("intent", 0.99)))
	check(t, d, gate.Review, "intent", true)
	if d.Value != 0 || d.Readings[0].Value != 0 {
		t.Errorf("value %v readings %+v", d.Value, d.Readings)
	}
	check(t, voiceBanking.Evaluate(gate.Inputs{}), gate.Escalate, "intent", true)
	score := gate.Input{Name: "intent", Kind: gate.KindScore, Probabilities: map[string]float64{"0": 1}, Confidence: 1, HasConfidence: true}
	check(t, voiceBanking.Evaluate(gate.NewInputs(score)), gate.Escalate, "intent", true)
}
