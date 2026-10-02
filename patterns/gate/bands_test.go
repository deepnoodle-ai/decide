package gate_test

import (
	"math"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

func TestBandsBounds(t *testing.T) {
	// Placeholder bounds.
	safe := gate.Bands{Input: "x", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.8, Review: 0.5}
	risky := gate.Bands{Input: "x", Measure: gate.MeasureNoul, Polarity: gate.HighIsRisky, Allow: 0.2, Review: 0.5}
	for _, tc := range []struct {
		name string
		rule gate.Bands
		v    float64
		want gate.Outcome
	}{
		{"safe at allow", safe, 0.8, gate.Allow},
		{"safe above allow", safe, up(0.8), gate.Allow},
		{"safe below allow", safe, down(0.8), gate.Review},
		{"safe at review", safe, 0.5, gate.Review},
		{"safe above review", safe, up(0.5), gate.Review},
		{"safe below review", safe, down(0.5), gate.Escalate},
		{"safe 0", safe, 0, gate.Escalate},
		{"safe 1", safe, 1, gate.Allow},
		{"risky at allow", risky, 0.2, gate.Allow},
		{"risky above allow", risky, up(0.2), gate.Review},
		{"risky below allow", risky, down(0.2), gate.Allow},
		{"risky at review", risky, 0.5, gate.Review},
		{"risky above review", risky, up(0.5), gate.Escalate},
		{"risky below review", risky, down(0.5), gate.Review},
		{"risky 1", risky, 1, gate.Escalate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.rule.Evaluate(gate.NewInputs(noul("x", tc.v)))
			check(t, d, tc.want, "x", false)
			if d.Value != tc.v || d.Rule != "bands" {
				t.Fatalf("value %v rule %q", d.Value, d.Rule)
			}
			if len(d.Readings) != 1 || d.Readings[0] != (gate.Reading{Rule: "bands", Input: "x", Measure: gate.MeasureNoul, Value: tc.v}) {
				t.Fatalf("readings %+v", d.Readings)
			}
		})
	}
}

func TestBandsReason(t *testing.T) {
	r := gate.Bands{Name: "path", Input: "x", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.9, Review: 0.6}
	d := r.Evaluate(gate.NewInputs(noul("x", 0.7)))
	if want := "x noul=0.7 -> review (allow 0.9, review 0.6, high_is_safe)"; d.Reason != want {
		t.Errorf("reason %q, want %q", d.Reason, want)
	}
	if d.Rule != "path" {
		t.Errorf("rule %q", d.Rule)
	}
}

func TestBandsProb(t *testing.T) {
	// P(delete) is risky: high P means more severe. Placeholder bounds.
	del := gate.Bands{Input: "action", Measure: gate.MeasureProb, Option: "delete", Polarity: gate.HighIsRisky, Allow: 0.1, Review: 0.3}
	in := choice("action", 0.6, map[string]float64{"read": 0.8, "delete": 0.2})
	d := del.Evaluate(gate.NewInputs(in))
	check(t, d, gate.Review, "action", false)
	if d.Readings[0].Option != "delete" || !strings.Contains(d.Reason, "prob(delete)=0.2") {
		t.Errorf("%+v %q", d.Readings, d.Reason)
	}
	// Absent option key is undefined, hence missing.
	d = gate.Bands{Input: "action", Measure: gate.MeasureProb, Option: "nope", Polarity: gate.HighIsRisky,
		Allow: 0.1, Review: 0.3, OnMissing: gate.Review}.Evaluate(gate.NewInputs(in))
	check(t, d, gate.Review, "action", true)

	// Noul "true" and "false".
	yes := gate.Bands{Input: "n", Measure: gate.MeasureProb, Option: "true", Polarity: gate.HighIsSafe, Allow: 0.9, Review: 0.5}
	no := yes
	no.Option = "false"
	n := gate.NewInputs(noul("n", 0.95))
	check(t, yes.Evaluate(n), gate.Allow, "n", false)
	d = no.Evaluate(n)
	check(t, d, gate.Escalate, "n", false)
	if !near(d.Value, 0.05) {
		t.Errorf("P(false) = %v", d.Value)
	}
}

func TestBandsOnMissing(t *testing.T) {
	inputs := map[string]struct {
		in      gate.Input
		problem string
		reason  string
	}{
		"missing":   {gate.Missing("x"), "missing", "x missing"},
		"failed":    {gate.Failed("x", errString("boom")), "failed", "x failed: boom"},
		"abstained": {gate.Abstained("x"), "abstained", "x abstained"},
		"bad value": {noul("x", math.NaN()), "bad_value", "x noul bad value"},
		"undefined": {choice("x", 0.9, map[string]float64{"a": 1}), "undefined", "x noul undefined for choice"},
	}
	modes := map[string]struct {
		set  gate.Outcome
		want gate.Outcome
	}{
		"unset":    {0, gate.Escalate},
		"allow":    {gate.Allow, gate.Allow},
		"review":   {gate.Review, gate.Review},
		"escalate": {gate.Escalate, gate.Escalate},
	}
	for iname, ic := range inputs {
		for mname, mc := range modes {
			t.Run(iname+"/"+mname, func(t *testing.T) {
				r := gate.Bands{Input: "x", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe,
					Allow: 0.9, Review: 0.5, OnMissing: mc.set}
				var in gate.Inputs
				if ic.in.Kind != gate.KindMissing {
					in = gate.NewInputs(ic.in)
				}
				d := r.Evaluate(in)
				check(t, d, mc.want, "x", true)
				if d.Value != 0 || d.Readings[0].Problem != ic.problem || d.Readings[0].Value != 0 {
					t.Fatalf("value %v readings %+v", d.Value, d.Readings)
				}
				if want := ic.reason + " -> " + mc.want.String(); d.Reason != want {
					t.Fatalf("reason %q, want %q", d.Reason, want)
				}
			})
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestValidate(t *testing.T) {
	ok := gate.Bands{Input: "x", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.9, Review: 0.5}
	mod := func(f func(*gate.Bands)) gate.Bands { r := ok; f(&r); return r }
	okMC := gate.MinConfidence{Inputs: []string{"a"}, Measure: gate.MeasureConfidence, Floor: 0.5}
	okAsym := gate.Asymmetric{Input: "a", Measure: gate.MeasureConfidence, Options: map[string]gate.OptionBand{"x": {Allow: 0.8, Review: 0.5}}}
	for _, tc := range []struct {
		name  string
		rule  gate.Validator
		valid bool
	}{
		{"bands ok", ok, true},
		{"bands risky ok", mod(func(r *gate.Bands) { r.Polarity, r.Allow, r.Review = gate.HighIsRisky, 0.1, 0.5 }), true},
		{"bands prob ok", mod(func(r *gate.Bands) { r.Measure, r.Option = gate.MeasureProb, "true" }), true},
		{"bands no input", mod(func(r *gate.Bands) { r.Input = "" }), false},
		{"bands no measure", mod(func(r *gate.Bands) { r.Measure = "" }), false},
		{"bands unknown measure", mod(func(r *gate.Bands) { r.Measure = "certainty" }), false},
		{"bands prob without option", mod(func(r *gate.Bands) { r.Measure = gate.MeasureProb }), false},
		{"bands option without prob", mod(func(r *gate.Bands) { r.Option = "true" }), false},
		{"bands no polarity", mod(func(r *gate.Bands) { r.Polarity = "" }), false},
		{"bands bad polarity", mod(func(r *gate.Bands) { r.Polarity = "high" }), false},
		{"bands safe inverted", mod(func(r *gate.Bands) { r.Allow, r.Review = 0.4, 0.5 }), false},
		{"bands risky inverted", mod(func(r *gate.Bands) { r.Polarity = gate.HighIsRisky }), false},
		{"bands NaN", mod(func(r *gate.Bands) { r.Allow = math.NaN() }), false},
		{"bands above 1", mod(func(r *gate.Bands) { r.Allow = 1.5 }), false},
		{"bands below 0", mod(func(r *gate.Bands) { r.Review = -0.1 }), false},
		{"bands bad outcome", mod(func(r *gate.Bands) { r.OnMissing = 7 }), false},
		{"allof no inputs", gate.AllOf{Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe}, false},
		{"allof repeated", gate.AllOf{Inputs: []string{"a", "a"}, Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe}, false},
		{"allof empty name", gate.AllOf{Inputs: []string{"a", ""}, Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe}, false},
		{"allof ok", gate.AllOf{Inputs: []string{"a", "b"}, Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe}, true},
		{"minconf ok", okMC, true},
		{"minconf noul refused", gate.MinConfidence{Inputs: []string{"a"}, Measure: gate.MeasureNoul}, false},
		{"minconf prob refused", gate.MinConfidence{Inputs: []string{"a"}, Measure: gate.MeasureProb}, false},
		{"minconf floor 2", gate.MinConfidence{Inputs: []string{"a"}, Measure: gate.MeasureTop, Floor: 2}, false},
		{"minconf bad below", gate.MinConfidence{Inputs: []string{"a"}, Measure: gate.MeasureTop, Below: 4}, false},
		{"margin ok", gate.Margin{Input: "a", Top: 0.8, Lead: 0.5}, true},
		{"margin no input", gate.Margin{Top: 0.8}, false},
		{"margin bad lead", gate.Margin{Input: "a", Lead: math.Inf(1)}, false},
		{"asym ok", okAsym, true},
		{"asym noul refused", gate.Asymmetric{Input: "a", Measure: gate.MeasureNoul}, false},
		{"asym inverted band", gate.Asymmetric{Input: "a", Measure: gate.MeasureTop, Options: map[string]gate.OptionBand{"x": {Allow: 0.5, Review: 0.8}}}, false},
		{"asym bad always", gate.Asymmetric{Input: "a", Measure: gate.MeasureTop, Options: map[string]gate.OptionBand{"x": {Always: 5}}}, false},
		{"asym empty key", gate.Asymmetric{Input: "a", Measure: gate.MeasureTop, Options: map[string]gate.OptionBand{"": {}}}, false},
		{"asym bad others", gate.Asymmetric{Input: "a", Measure: gate.MeasureTop, Others: &gate.OptionBand{Allow: 2}}, false},
		{"asym bad floor", gate.Asymmetric{Input: "a", Measure: gate.MeasureTop, Floor: -1}, false},
		{"compose empty", gate.Compose{}, false},
		{"compose nil child", gate.Compose{Rules: []gate.Rule{nil}}, false},
		{"compose nil pointer child", gate.Compose{Rules: []gate.Rule{(*gate.Bands)(nil)}}, false},
		{"compose invalid child", gate.Compose{Rules: []gate.Rule{ok, gate.Margin{}}}, false},
		{"compose invalid custom", gate.Compose{Rules: []gate.Rule{badCustom{}}}, false},
		{"compose ok", gate.Compose{Rules: []gate.Rule{ok, gate.Compose{Rules: []gate.Rule{okMC}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rule.Validate()
			if tc.valid != (err == nil) {
				t.Fatalf("Validate() = %v, want valid=%v", err, tc.valid)
			}
			if err != nil && !isInvalidRule(err) {
				t.Fatalf("error %v does not wrap ErrInvalidRule", err)
			}
			if !tc.valid {
				d := tc.rule.(gate.Rule).Evaluate(gate.NewInputs(noul("x", 1), noul("a", 1)))
				if d.Outcome != gate.Escalate || !strings.HasPrefix(d.Reason, "invalid rule: ") {
					t.Fatalf("invalid rule evaluated to %s %q", d.Outcome, d.Reason)
				}
			}
		})
	}
}

type badCustom struct{}

func (badCustom) Evaluate(gate.Inputs) gate.Decision { return gate.Decision{Outcome: gate.Allow} }
func (badCustom) Validate() error                    { return errString("custom config broken") }
