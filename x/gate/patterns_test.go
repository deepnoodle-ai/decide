package gate_test

import (
	"math"
	"testing"

	"github.com/deepnoodle-ai/decide/x/gate"
)

// Common hand-written gating rules, each written as a composition of v0
// primitives and compared with a direct transcription of the rule on the
// same inputs. Every number is a placeholder, not a recommendation.

// A cache-hit check: min() over five atomic Nouls; any error scores 0.0,
// so a failure is a miss. Hit when min >= threshold.
func TestPatternMinOfNoulsFailsClosed(t *testing.T) {
	const threshold = 0.8
	original := func(ps []float64, errs []bool) bool {
		m := math.Inf(1)
		for i, p := range ps {
			if errs[i] {
				p = 0
			}
			m = min(m, p)
		}
		return m >= threshold
	}
	rule := gate.AllOf{Inputs: []string{"q1", "q2", "q3", "q4", "q5"}, Measure: gate.MeasureNoul,
		Polarity: gate.HighIsSafe, Allow: threshold, Review: threshold}
	for _, tc := range []struct {
		ps   []float64
		errs []bool
	}{
		{[]float64{0.9, 0.9, 0.9, 0.9, 0.9}, make([]bool, 5)},
		{[]float64{0.9, 0.9, 0.79, 0.9, 0.9}, make([]bool, 5)},
		{[]float64{0.8, 0.8, 0.8, 0.8, 0.8}, make([]bool, 5)},
		{[]float64{0.99, 0.99, 0.99, 0.99, 0.99}, []bool{false, false, false, true, false}},
		{[]float64{0.1, 0.2, 0.3, 0.4, 0.5}, make([]bool, 5)},
	} {
		var ins []gate.Input
		for i, p := range tc.ps {
			name := rule.Inputs[i]
			if tc.errs[i] {
				ins = append(ins, gate.Failed(name, errString("api error")))
			} else {
				ins = append(ins, noul(name, p))
			}
		}
		got := rule.Evaluate(gate.NewInputs(ins...)).Outcome == gate.Allow
		if want := original(tc.ps, tc.errs); got != want {
			t.Errorf("%v %v: hit=%v, original %v", tc.ps, tc.errs, got, want)
		}
	}
}

// An irreversible action: click when min(kind, item) confidence >= 0.4; a
// recoverable answer ("scroll") is left out.
func TestPatternMinOverIrreversibleAnswers(t *testing.T) {
	original := func(kind, item, _ float64) bool { return min(kind, item) >= 0.4 }
	rule := gate.MinConfidence{Inputs: []string{"kind", "item"}, Measure: gate.MeasureConfidence, Floor: 0.4}
	for _, c := range [][3]float64{{0.9, 0.9, 0.1}, {0.4, 0.9, 0.9}, {0.39, 0.9, 0.9}, {0.9, 0.2, 0.9}, {0.5, 0.5, 0}} {
		in := gate.NewInputs(conf("kind", c[0]), conf("item", c[1]), conf("scroll", c[2]))
		got := rule.Evaluate(in).Outcome == gate.Allow
		if want := original(c[0], c[1], c[2]); got != want {
			t.Errorf("%v: click=%v, original %v", c, got, want)
		}
	}
}

// Top plus lead: auto-accept when top >= 0.85 && top - runnerUp >= 0.5,
// else "review".
func TestPatternTopAndLead(t *testing.T) {
	original := func(ps []float64) string {
		top, runner := math.Inf(-1), math.Inf(-1)
		for _, p := range ps {
			if p > top {
				top, runner = p, top
			} else if p > runner {
				runner = p
			}
		}
		if top >= 0.85 && top-runner >= 0.5 {
			return "accept"
		}
		return "review"
	}
	rule := gate.Margin{Input: "label", Top: 0.85, Lead: 0.5, Below: gate.Review}
	for _, ps := range [][]float64{{0.9, 0.05, 0.05}, {0.85, 0.1, 0.05}, {0.84, 0.1, 0.06}, {0.6, 0.3, 0.1}, {0.9, 0.5}} {
		probs := map[string]float64{}
		for i, p := range ps {
			probs[string(rune('a'+i))] = p
		}
		in := gate.Input{Name: "label", Kind: gate.KindChoice, Probabilities: probs}
		got := map[gate.Outcome]string{gate.Allow: "accept", gate.Review: "review"}[rule.Evaluate(gate.NewInputs(in)).Outcome]
		if want := original(ps); got != want {
			t.Errorf("%v: %s, original %s", ps, got, want)
		}
	}
}

// A router: never take the risky branch (a model downgrade) when confidence
// is below 0.3; fail open to stay off the hot path. Allow means "take the
// chosen branch"; anything else means "take the safe branch".
func TestPatternRiskyBranch(t *testing.T) {
	original := func(choice string, conf float64, ok bool) bool { // take chosen branch?
		if !ok {
			return true // fail open
		}
		return !(choice == "downgrade" && conf < 0.3)
	}
	rule := gate.Asymmetric{Input: "tier", Measure: gate.MeasureConfidence,
		Options:   map[string]gate.OptionBand{"downgrade": {Allow: 0.3, Review: 0.3}},
		Others:    new(gate.OptionBand{Always: gate.Allow}),
		OnMissing: gate.Allow}
	for _, tc := range []struct {
		choice string
		conf   float64
		ok     bool
	}{{"downgrade", 0.5, true}, {"downgrade", 0.3, true}, {"downgrade", 0.29, true}, {"keep", 0.1, true}, {"upgrade", 0.05, true}, {"", 0, false}} {
		var in gate.Inputs
		if tc.ok {
			in = gate.NewInputs(gate.Input{Name: "tier", Kind: gate.KindChoice, Choice: tc.choice,
				Probabilities: map[string]float64{tc.choice: 0.6, "other": 0.4}, Confidence: tc.conf, HasConfidence: true})
		}
		d := rule.Evaluate(in)
		got := d.Outcome == gate.Allow
		if want := original(tc.choice, tc.conf, tc.ok); got != want {
			t.Errorf("%+v: take=%v, original %v (%s)", tc, got, want, d.Reason)
		}
	}
}

// A review check: certainty = min(score.confidence,
// 0.5 + |applicability - 0.5|); act when certainty >= threshold, else review.
func TestPatternApplicabilityCertainty(t *testing.T) {
	const threshold = 0.7
	original := func(scoreConf, applicability float64) bool {
		return min(scoreConf, 0.5+math.Abs(applicability-0.5)) >= threshold
	}
	rule := gate.Compose{Name: "review", Rules: []gate.Rule{
		gate.MinConfidence{Inputs: []string{"severity"}, Measure: gate.MeasureConfidence, Floor: threshold, Below: gate.Review},
		gate.MinConfidence{Inputs: []string{"applies"}, Measure: gate.MeasureApplicability, Floor: threshold, Below: gate.Review},
	}}
	for _, c := range [][2]float64{{0.9, 0.95}, {0.9, 0.05}, {0.9, 0.5}, {0.6, 0.99}, {0.7, 0.2}, {0.7, 0.3}} {
		sev := gate.Input{Name: "severity", Kind: gate.KindScore, Probabilities: map[string]float64{"0": 0.5, "1": 0.5},
			Confidence: c[0], HasConfidence: true}
		d := rule.Evaluate(gate.NewInputs(sev, noul("applies", c[1])))
		if got, want := d.Outcome == gate.Allow, original(c[0], c[1]); got != want {
			t.Errorf("%v: act=%v, original %v (%s)", c, got, want, d.Reason)
		}
		if d.Outcome != gate.Allow && d.Outcome != gate.Review {
			t.Errorf("%v: %s", c, d.Outcome)
		}
	}
}

// Docs three bands (https://docs.typesafe.ai/patterns/confidence-routing.md,
// the approve_transfer branch): below 0.6 escalate, above 0.85 act, else
// confirm. The docs compare "> 0.85", so the allow bound is up(0.85).
func TestPatternDocsThreeBands(t *testing.T) {
	original := func(c float64) gate.Outcome {
		switch {
		case c < 0.6:
			return gate.Escalate
		case c > 0.85:
			return gate.Allow
		}
		return gate.Review
	}
	rule := gate.Bands{Input: "intent", Measure: gate.MeasureConfidence, Polarity: gate.HighIsSafe, Allow: up(0.85), Review: 0.6}
	for _, c := range []float64{0, 0.3, down(0.6), 0.6, 0.7, 0.85, up(0.85), 0.9, 1} {
		if got, want := rule.Evaluate(gate.NewInputs(conf("intent", c))).Outcome, original(c); got != want {
			t.Errorf("%v: %s, original %s", c, got, want)
		}
	}
}

// Function-calling cookbook: a call's confidence is its least certain
// argument, not the product.
func TestPatternFunctionCallingMin(t *testing.T) {
	const threshold = 0.6
	original := func(cs ...float64) bool {
		m := math.Inf(1)
		for _, c := range cs {
			m = min(m, c)
		}
		return m >= threshold
	}
	rule := gate.MinConfidence{Inputs: []string{"tool", "arg_path", "arg_mode"}, Measure: gate.MeasureConfidence, Floor: threshold}
	for _, cs := range [][]float64{{0.9, 0.8, 0.7}, {0.9, 0.9, 0.59}, {0.6, 0.6, 0.6}, {0.8, 0.8, 0.8}} {
		// The product would reject {0.8, 0.8, 0.8} (0.512); the minimum
		// accepts it.
		in := gate.NewInputs(conf("tool", cs[0]), conf("arg_path", cs[1]), conf("arg_mode", cs[2]))
		if got, want := rule.Evaluate(in).Outcome == gate.Allow, original(cs...); got != want {
			t.Errorf("%v: %v, original %v", cs, got, want)
		}
	}
}
