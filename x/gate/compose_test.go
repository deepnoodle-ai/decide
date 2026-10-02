package gate_test

import (
	"sync"
	"testing"

	"github.com/deepnoodle-ai/decide/x/gate"
)

// custom is a rule outside the package.
type custom struct {
	rule    string
	outcome gate.Outcome
}

func (c custom) Evaluate(gate.Inputs) gate.Decision {
	return gate.Decision{Outcome: c.outcome, Rule: c.rule, Reason: "custom says so",
		Readings: []gate.Reading{{Rule: c.rule, Input: "ctx", Measure: "tokens", Value: 0.5}}}
}

func TestCompose(t *testing.T) {
	allowA := gate.Bands{Input: "a", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.5, Review: 0.2}
	reviewB := gate.Bands{Input: "b", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.9, Review: 0.2}
	reviewC := gate.Bands{Name: "c_rule", Input: "c", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, Allow: 0.9, Review: 0.2}
	in := gate.NewInputs(noul("a", 0.8), noul("b", 0.5), noul("c", 0.5))

	t.Run("most severe wins", func(t *testing.T) {
		d := gate.Compose{Rules: []gate.Rule{allowA, reviewB}}.Evaluate(in)
		check(t, d, gate.Review, "b", false)
		if d.Rule != "compose/bands[1]" {
			t.Fatalf("rule %q", d.Rule)
		}
		if len(d.Readings) != 2 || d.Readings[0].Rule != "compose/bands[0]" || d.Readings[1].Rule != "compose/bands[1]" {
			t.Fatalf("readings %+v", d.Readings)
		}
	})
	t.Run("tie picks earliest", func(t *testing.T) {
		d := gate.Compose{Rules: []gate.Rule{allowA, reviewC, reviewB}}.Evaluate(in)
		check(t, d, gate.Review, "c", false)
		if d.Rule != "compose/c_rule" || d.Reason != "c noul=0.5 -> review (allow 0.9, review 0.2, high_is_safe)" {
			t.Fatalf("rule %q reason %q", d.Rule, d.Reason)
		}
	})
	t.Run("nested paths", func(t *testing.T) {
		click := gate.Compose{Name: "click", Rules: []gate.Rule{
			gate.MinConfidence{Inputs: []string{"kind"}, Measure: gate.MeasureConfidence, Floor: 0.4},
			gate.Margin{Input: "kind", Top: 0.5, Lead: 0.1},
		}}
		policy := gate.Compose{Name: "policy", Rules: []gate.Rule{allowA, click}}
		d := policy.Evaluate(gate.NewInputs(noul("a", 0.9), conf("kind", 0.3)))
		check(t, d, gate.Escalate, "kind", false)
		if d.Rule != "policy/click/min_confidence[0]" {
			t.Fatalf("rule %q", d.Rule)
		}
		want := []string{"policy/bands[0]", "policy/click/min_confidence[0]", "policy/click/margin[1]", "policy/click/margin[1]"}
		if len(d.Readings) != len(want) {
			t.Fatalf("readings %+v", d.Readings)
		}
		for i, w := range want {
			if d.Readings[i].Rule != w {
				t.Fatalf("reading %d rule %q, want %q", i, d.Readings[i].Rule, w)
			}
		}
	})
	t.Run("missing child decides", func(t *testing.T) {
		d := gate.Compose{Rules: []gate.Rule{allowA, reviewB}}.Evaluate(gate.NewInputs(noul("a", 0.9)))
		check(t, d, gate.Escalate, "b", true)
	})
	t.Run("empty is invalid", func(t *testing.T) {
		d := gate.Compose{Name: "p"}.Evaluate(in)
		if d.Outcome != gate.Escalate || d.Rule != "p" || d.Reason == "" {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("custom child", func(t *testing.T) {
		d := gate.Compose{Name: "p", Rules: []gate.Rule{allowA, custom{rule: "ctx_size", outcome: gate.Review}}}.Evaluate(in)
		check(t, d, gate.Review, "", false)
		if d.Rule != "p/custom[1]/ctx_size" || d.Readings[1].Rule != "p/custom[1]/ctx_size" || d.Reason != "custom says so" {
			t.Fatalf("%+v", d)
		}
		// Unnamed custom rule; zero outcome counts as Escalate.
		d = gate.Compose{Rules: []gate.Rule{custom{}, allowA}}.Evaluate(in)
		if d.Outcome != gate.Escalate || d.Rule != "compose/custom[0]" {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("pointer children", func(t *testing.T) {
		d := gate.Compose{Rules: []gate.Rule{&allowA, &reviewB}}.Evaluate(in)
		check(t, d, gate.Review, "b", false)
		if d.Rule != "compose/bands[1]" {
			t.Fatalf("rule %q", d.Rule)
		}
	})
}

// Rules and Inputs are values that many goroutines can share. Run with -race.
func TestConcurrentEvaluate(t *testing.T) {
	policy := gate.Compose{Name: "p", Rules: []gate.Rule{voiceBanking, fiveNouls,
		gate.MinConfidence{Inputs: []string{"intent"}, Measure: gate.MeasureEntropy, Floor: 0.1}}}
	in := gate.NewInputs(intent("approve_transfer", 0.7), noul("q1", 0.9))
	want := policy.Evaluate(in)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 50 {
				if d := policy.Evaluate(in); d.Outcome != want.Outcome || d.Rule != want.Rule || d.Value != want.Value {
					t.Errorf("got %+v", d)
					return
				}
			}
		})
	}
	wg.Wait()
}
