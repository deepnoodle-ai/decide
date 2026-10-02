package gate_test

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// Placeholder bounds throughout.
var builtins = map[string]gate.Rule{
	"bands": gate.Bands{Name: "path_ok", Input: "path_ok", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe,
		Allow: 0.9, Review: 0.6, OnMissing: gate.Review},
	"bands prob zero bounds": gate.Bands{Input: "action", Measure: gate.MeasureProb, Option: "delete_file",
		Polarity: gate.HighIsRisky, Allow: 0, Review: 0},
	"all_of":         fiveNouls,
	"min_confidence": gate.MinConfidence{Inputs: []string{"kind", "item"}, Measure: gate.MeasureConfidence, Floor: 0.4, Below: gate.Review, OnMissing: gate.Allow},
	"margin":         gate.Margin{Name: "mcp", Input: "label", Top: 0.85, Lead: 0.5, Below: gate.Review},
	"asymmetric":     voiceBanking,
	"asymmetric others": gate.Asymmetric{Input: "intent", Measure: gate.MeasureTop, BelowFloor: gate.Review,
		Options: map[string]gate.OptionBand{"none": {Always: gate.Escalate}},
		Others:  new(gate.OptionBand{Allow: 0.6, Review: 0.3})},
	"compose nested": gate.Compose{Name: "policy", Rules: []gate.Rule{
		voiceBanking,
		gate.Compose{Name: "click", Rules: []gate.Rule{
			gate.MinConfidence{Inputs: []string{"kind"}, Measure: gate.MeasureEntropy, Floor: 0.2},
			gate.Margin{Input: "kind", Top: 0.5, Lead: 0.1},
		}},
		fiveNouls,
	}},
}

// A fixed input table every rule is evaluated on before and after the
// round trip.
var jsonInputs = []gate.Inputs{
	{},
	gate.NewInputs(noul("path_ok", 0.95), intent("approve_transfer", 0.9), conf("kind", 0.5), conf("item", 0.3)),
	gate.NewInputs(noul("path_ok", 0.7), intent("check_balance", 0.61), noul("q1", 0.9), noul("q2", 0.9),
		noul("q3", 0.9), noul("q4", 0.9), noul("q5", 0.85), choice("label", 0.8, map[string]float64{"a": 0.9, "b": 0.1})),
	gate.NewInputs(noul("path_ok", math.NaN()), intent("support", 0.99), gate.Failed("kind", errString("x")),
		choice("action", 0.5, map[string]float64{"delete_file": 0.3, "read_file": 0.7})),
	gate.NewInputs(noul("intent", 0.9), gate.Abstained("label"), noul("action", 0.5)),
}

func TestJSONRoundTrip(t *testing.T) {
	for name, rule := range builtins {
		t.Run(name, func(t *testing.T) {
			if err := rule.(gate.Validator).Validate(); err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(rule)
			if err != nil {
				t.Fatal(err)
			}
			b1, err := jsonv1.Marshal(rule)
			if err != nil || string(b1) != string(b) {
				t.Fatalf("v1 and v2 differ:\n%s\n%s (%v)", b, b1, err)
			}
			if !strings.HasPrefix(string(b), `{"type":"`) {
				t.Fatalf("type not first: %s", b)
			}
			back, err := gate.DecodeRule(b)
			if err != nil {
				t.Fatalf("DecodeRule(%s): %v", b, err)
			}
			if !reflect.DeepEqual(back, rule) {
				t.Fatalf("round trip differs:\n%#v\n%#v", rule, back)
			}
			again, _ := json.Marshal(back)
			if string(again) != string(b) {
				t.Fatalf("re-encoding differs:\n%s\n%s", b, again)
			}
			for i, in := range jsonInputs {
				d1, d2 := rule.Evaluate(in), back.Evaluate(in)
				if !reflect.DeepEqual(d1, d2) {
					t.Fatalf("input %d: decisions differ:\n%+v\n%+v", i, d1, d2)
				}
				if _, err := json.Marshal(d1); err != nil {
					t.Fatalf("input %d: decision does not encode: %v", i, err)
				}
			}
		})
	}
}

func TestJSONUnmarshalDirect(t *testing.T) {
	// Callers can use either json package on a concrete rule type.
	src := `{"type":"margin","input":"x","top":0.8,"lead":0.4}`
	var v1 gate.Margin
	if err := jsonv1.Unmarshal([]byte(src), &v1); err != nil {
		t.Fatal(err)
	}
	var v2 gate.Margin
	if err := json.Unmarshal([]byte(src), &v2); err != nil {
		t.Fatal(err)
	}
	if want := (gate.Margin{Input: "x", Top: 0.8, Lead: 0.4}); v1 != want || v2 != want {
		t.Fatalf("%+v %+v", v1, v2)
	}
	// "type" may be omitted on a concrete type, but must match if present.
	if err := json.Unmarshal([]byte(`{"input":"x","top":0.8,"lead":0.4}`), &v2); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"type":"bands","input":"x","top":0.8,"lead":0.4}`), &v2); err == nil {
		t.Fatal("wrong type accepted")
	}
	// An option band with "always" needs no bounds.
	var ob gate.OptionBand
	if err := json.Unmarshal([]byte(`{"always":"review"}`), &ob); err != nil || ob.Always != gate.Review {
		t.Fatalf("%+v %v", ob, err)
	}
}

func TestDecodeRuleErrors(t *testing.T) {
	bands := `"input":"x","measure":"noul","polarity":"high_is_safe"`
	for _, tc := range []struct {
		name string
		src  string
		is   error
	}{
		{"unknown member", `{"type":"bands",` + bands + `,"allow":0.9,"review":0.5,"alow":0.9}`, nil},
		{"duplicate member", `{"type":"bands",` + bands + `,"allow":0.9,"review":0.5,"allow":0.1}`, nil},
		{"duplicate type", `{"type":"bands","type":"margin",` + bands + `,"allow":0.9,"review":0.5}`, nil},
		{"unknown type", `{"type":"any_of","inputs":["a"]}`, gate.ErrUnknownRule},
		{"missing type", `{` + bands + `,"allow":0.9,"review":0.5}`, gate.ErrUnknownRule},
		{"invalid bound", `{"type":"bands",` + bands + `,"allow":1.5,"review":0.5}`, gate.ErrInvalidRule},
		{"inverted bounds", `{"type":"bands",` + bands + `,"allow":0.4,"review":0.5}`, gate.ErrInvalidRule},
		{"missing bound", `{"type":"bands",` + bands + `,"allow":0.9}`, nil},
		{"bad outcome string", `{"type":"bands",` + bands + `,"allow":0.9,"review":0.5,"on_missing":"alow"}`, nil},
		{"capitalized outcome", `{"type":"margin","input":"x","top":0.8,"lead":0.4,"below":"Review"}`, nil},
		{"outcome as number", `{"type":"margin","input":"x","top":0.8,"lead":0.4,"below":2}`, nil},
		{"bound as string", `{"type":"margin","input":"x","top":"0.8","lead":0.4}`, nil},
		{"unknown measure", `{"type":"min_confidence","inputs":["a"],"measure":"certainty","floor":0.4}`, gate.ErrInvalidRule},
		{"empty compose", `{"type":"compose","rules":[]}`, gate.ErrInvalidRule},
		{"compose bad child", `{"type":"compose","rules":[{"type":"margin","input":"x","top":0.8,"lead":0.4,"extra":1}]}`, nil},
		{"compose unknown child", `{"type":"compose","rules":[{"type":"custom"}]}`, gate.ErrUnknownRule},
		{"compose invalid child", `{"type":"compose","rules":[{"type":"margin","input":"","top":0.8,"lead":0.4}]}`, gate.ErrInvalidRule},
		{"option band unknown member", `{"type":"asymmetric","input":"x","measure":"top","floor":0,"options":{"a":{"allow":0.5,"reviw":0.5}}}`, nil},
		{"option band missing bound", `{"type":"asymmetric","input":"x","measure":"top","floor":0,"options":{"a":{"allow":0.5}}}`, nil},
		{"not an object", `[1,2]`, nil},
		{"not JSON", `{"type":`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := gate.DecodeRule([]byte(tc.src))
			if err == nil {
				t.Fatalf("decoded %#v", r)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("err %v, want %v", err, tc.is)
			}
		})
	}
}

func TestComposeMarshalCustomFails(t *testing.T) {
	c := gate.Compose{Rules: []gate.Rule{fiveNouls, custom{rule: "x"}}}
	if _, err := json.Marshal(c); err == nil {
		t.Fatal("custom child marshaled")
	}
	if _, err := json.Marshal(gate.Bands{Input: "x", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe, OnMissing: 9}); err == nil {
		t.Fatal("invalid outcome marshaled")
	}
}
