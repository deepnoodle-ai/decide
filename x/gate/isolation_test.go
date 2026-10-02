package gate_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/decide/x/gate"
)

type sharedReadingsRule struct {
	readings []gate.Reading
}

func (r sharedReadingsRule) Evaluate(gate.Inputs) gate.Decision {
	return gate.Decision{Outcome: gate.Allow, Rule: "original", Readings: r.readings}
}

func TestComposeOwnsCustomReadings(t *testing.T) {
	readings := []gate.Reading{{Rule: "original", Input: "q", Value: .9}}
	want := append([]gate.Reading(nil), readings...)
	rule := gate.Compose{Rules: []gate.Rule{sharedReadingsRule{readings}}}
	check := func() {
		t.Helper()
		d := rule.Evaluate(gate.Inputs{})
		if len(d.Readings) != 1 || d.Readings[0].Rule != "compose/custom[0]/original" {
			t.Errorf("unexpected reading paths: %+v", d.Readings)
		}
	}
	check()
	check()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				check()
			}
		})
	}
	wg.Wait()
	if !reflect.DeepEqual(readings, want) {
		t.Fatalf("custom readings changed: %+v", readings)
	}
}

func TestComposeMarshalRejectsNilChildren(t *testing.T) {
	for _, child := range []gate.Rule{nil, (*gate.Bands)(nil)} {
		if _, err := json.Marshal(gate.Compose{Rules: []gate.Rule{child}}); err == nil {
			t.Fatalf("marshaled nil child %T", child)
		}
	}
}

func TestInputsOwnProbabilityMaps(t *testing.T) {
	probabilities := map[string]float64{"yes": .9, "no": .1}
	in := gate.NewInputs(gate.Input{Name: "q", Kind: gate.KindChoice, Probabilities: probabilities})
	rule := gate.Bands{Input: "q", Measure: gate.MeasureProb, Option: "yes", Polarity: gate.HighIsSafe, Allow: .8, Review: .5}
	probabilities["yes"] = .1
	in.Get("q").Probabilities["yes"] = .2
	for _, input := range in.All() {
		input.Probabilities["yes"] = .3
	}
	if d := rule.Evaluate(in); d.Outcome != gate.Allow || d.Value != .9 {
		t.Fatalf("external mutation changed decision: %+v", d)
	}
}

func TestDecodeRuleRejectsNullBounds(t *testing.T) {
	cases := []string{
		`{"type":"bands","input":"q","measure":"noul","polarity":"high_is_safe","allow":null,"review":0}`,
		`{"type":"bands","input":"q","measure":"noul","polarity":"high_is_safe","allow":0,"review":null}`,
		`{"type":"all_of","inputs":["q"],"measure":"noul","polarity":"high_is_safe","allow":null,"review":0}`,
		`{"type":"min_confidence","inputs":["q"],"measure":"confidence","floor":null}`,
		`{"type":"margin","input":"q","top":null,"lead":0}`,
		`{"type":"margin","input":"q","top":0,"lead":null}`,
		`{"type":"asymmetric","input":"q","measure":"confidence","floor":null,"options":{"yes":{"always":"allow"}}}`,
		`{"type":"asymmetric","input":"q","measure":"confidence","floor":0,"options":{"yes":{"allow":null,"review":0}}}`,
		`{"type":"asymmetric","input":"q","measure":"confidence","floor":0,"options":{"yes":{"allow":0,"review":null}}}`,
	}
	for _, raw := range cases {
		if _, err := gate.DecodeRule([]byte(raw)); err == nil {
			t.Fatalf("accepted null bound: %s", raw)
		}
		if strings.Contains(raw, `"null"`) {
			t.Fatal("test must use a JSON null")
		}
	}
	if _, err := gate.DecodeRule([]byte(`{"type":"bands","input":"q","measure":"noul","polarity":"high_is_safe","allow":0,"review":0}`)); err != nil {
		t.Fatalf("explicit zero rejected: %v", err)
	}
}
