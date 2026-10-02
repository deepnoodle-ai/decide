package gate_test

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/gate"
)

// Gate a tool call on a Choice and a Noul. Every number here is a
// placeholder; measure thresholds on your own data.
func Example() {
	policy := gate.Compose{Name: "file_op", Rules: []gate.Rule{
		gate.Asymmetric{Input: "action", Measure: gate.MeasureConfidence,
			Floor: 0.6, // placeholder
			Options: map[string]gate.OptionBand{
				"read_file":   {Allow: 0.6, Review: 0.6},  // placeholder
				"delete_file": {Allow: 0.85, Review: 0.6}, // placeholder
				"none":        {Always: gate.Escalate},
			}},
		gate.Bands{Input: "path_ok", Measure: gate.MeasureNoul, Polarity: gate.HighIsSafe,
			Allow: 0.9, Review: 0.6}, // placeholders
	}}

	in := gate.NewInputs(
		gate.Input{Name: "action", Kind: gate.KindChoice, Choice: "delete_file",
			Probabilities: map[string]float64{"read_file": 0.12, "delete_file": 0.8, "none": 0.08},
			Confidence:    0.7, HasConfidence: true},
		gate.Input{Name: "path_ok", Kind: gate.KindNoul, Noul: 0.95},
	)
	d := policy.Evaluate(in)
	fmt.Println(d.Outcome)
	fmt.Println(d.Rule)
	fmt.Println(d.Reason)
	// Output:
	// review
	// file_op/asymmetric[0]
	// action chose delete_file, confidence=0.7 -> review (review 0.6)
}

// Keep a policy in a config file. Unknown members, duplicate members, and
// missing bounds are rejected, so a typo cannot read as a 0 bound.
func ExampleDecodeRule() {
	rule, err := gate.DecodeRule([]byte(`{
		"type": "margin",
		"input": "label",
		"top": 0.85,
		"lead": 0.5,
		"below": "review"
	}`)) // placeholder bounds
	if err != nil {
		log.Fatal(err)
	}
	in := gate.NewInputs(gate.Input{Name: "label", Kind: gate.KindChoice,
		Probabilities: map[string]float64{"bug": 0.8, "feature": 0.15, "question": 0.05}})
	fmt.Println(rule.Evaluate(in).Reason)

	_, err = gate.DecodeRule([]byte(`{"type":"margin","input":"label","top":0.85,"laed":0.5}`))
	fmt.Println(err != nil)
	// Output:
	// label top=0.8 (need 0.85) margin=0.65 (need 0.5) -> review
	// true
}

// Adapt a whole response, including answers that failed validation. A
// missing answer fails closed.
func ExampleFromResponse() {
	srv, err := decidetest.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()
	srv.Respond(func(*decide.Request) (*decide.Response, error) {
		// "path_ok" is omitted.
		return &decide.Response{Answers: map[string]decide.Answer{
			"action": decidetest.ChoiceAnswer(map[string]float64{"read_file": 0.9, "delete_file": 0.1}),
		}}, nil
	})
	client, err := srv.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	req := decide.NewRequest("Show me notes.txt")
	decide.Ask(req, "action", decide.Choice("Which operation?",
		decide.Option("read_file"), decide.Option("delete_file")))
	decide.Ask(req, "path_ok", decide.Noul("Is the path the one the user named?"))

	resp, err := client.SystemOne(context.Background(), req)
	fmt.Println("error:", err != nil) // log it; resp is still usable

	policy := gate.AllOf{Inputs: []string{"path_ok"}, Measure: gate.MeasureNoul,
		Polarity: gate.HighIsSafe, Allow: 0.9, Review: 0.6} // placeholders
	d := policy.Evaluate(gate.FromResponse(resp))
	fmt.Println(d.Outcome, d.Weakest, d.Missing)
	// Output:
	// error: true
	// escalate path_ok true
}
