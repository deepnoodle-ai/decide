// Command calibrate fits a cutoff on saved, labeled answers and checks it
// on separate held-out cases. It runs offline with invented fixture data.
//
//	go run ./examples/calibrate
//
// Six cases illustrate the API; they are too few to choose a real cutoff.
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/calibrate"
	"github.com/deepnoodle-ai/decide/x/gate"
)

func main() {
	question, err := json.Marshal(decide.Noul("Does this message need an immediate response?"))
	if err != nil {
		log.Fatal(err)
	}
	fit := calibrate.Dataset{
		QuestionKey: "urgent", Question: question, Model: "fixture-model",
		Cases: []calibrate.Case{
			fixture("f1", "Production is down.", 0.9, "true"),
			fixture("f2", "Can you add dark mode?", 0.8, "false"),
			fixture("f3", "Payments fail for every user.", 0.7, "true"),
		},
	}
	heldout := fit // same question and model, different cases
	heldout.Cases = []calibrate.Case{
		fixture("h1", "Nobody can log in.", 0.95, "true"),
		fixture("h2", "Question about my invoice.", 0.92, "false"),
		fixture("h3", "Love the new release!", 0.2, "false"),
	}

	// Require at least one allowed fit case and no errors among allowed fit cases.
	// Held-out errors can still occur; fitting does not guarantee future accuracy.
	a, err := calibrate.Fit(fit, heldout, calibrate.Config{
		Measure: gate.MeasureNoul, NoulTarget: "true", MaxError: 0, MinAllowed: 1, ECEBins: 5,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("flag urgent when noul >= %.2f\n", a.Cutoff)
	fmt.Printf("held-out: %d of %d flagged, %d wrong\n",
		a.Baseline.Allowed, a.Baseline.Count, a.Baseline.Errors)
}

// In real use, save State and Answer from requests to one resolved model
// version, then obtain labels independently of the model's predictions.
func fixture(id, text string, p float64, label string) calibrate.Case {
	state, err := json.Marshal(text)
	if err != nil {
		log.Fatal(err)
	}
	answer, err := json.Marshal(&decide.NoulAnswer{Noul: p})
	if err != nil {
		log.Fatal(err)
	}
	return calibrate.Case{ID: id, State: state, Answer: answer, Label: label}
}
