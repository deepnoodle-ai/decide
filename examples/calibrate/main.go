// Command calibrate asks the model whether each of ten labeled messages is
// urgent, fits an allow cutoff on the first half, and reports how that
// cutoff does on the second half.
//
//	TYPESAFE_API_KEY=... go run ./examples/calibrate
//
// Ten cases illustrate the API; they are too few to choose a real cutoff.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/calibrate"
	"github.com/deepnoodle-ai/decide/x/gate"
)

var messages = []struct{ text, urgent string }{
	{"Production is down.", "true"},
	{"Can you add dark mode?", "false"},
	{"Payments fail for every user.", "true"},
	{"Thanks for the fix.", "false"},
	{"I prefer the old menu.", "false"},
	{"The API returns 500 for every request.", "true"},
	{"Question about my invoice.", "false"},
	{"Our data export leaked customer emails.", "true"},
	{"Love the new release!", "false"},
	{"Nobody on the team can log in.", "true"},
}

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	question := decide.Noul("Does this message need an immediate response?")
	var cases []calibrate.Case
	var model string
	for i, m := range messages {
		req := decide.NewRequest(m.text)
		urgent := decide.Ask(req, "urgent", question)
		resp, err := client.SystemOne(context.Background(), req)
		if err != nil {
			log.Fatal(err)
		}
		a, err := urgent.From(resp)
		if err != nil {
			log.Fatal(err)
		}
		state, _ := json.Marshal(m.text)
		answer, _ := json.Marshal(a)
		cases = append(cases, calibrate.Case{
			ID: fmt.Sprint(i), State: state, Answer: answer, Label: m.urgent,
		})
		model = resp.Model
	}

	questionJSON, _ := json.Marshal(question)
	fit := calibrate.Dataset{
		QuestionKey: "urgent", Question: questionJSON, Model: model, Cases: cases[:5],
	}
	heldout := fit
	heldout.Cases = cases[5:]

	// Allow only when no fit case above the cutoff was mislabeled.
	a, err := calibrate.Fit(fit, heldout, calibrate.Config{
		Measure: gate.MeasureNoul, NoulTarget: "true", MaxError: 0, MinAllowed: 1, ECEBins: 5,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("model %s: allow when noul >= %.2f\n", a.Model, a.Cutoff)
	fmt.Printf("held-out: %d of %d allowed, %d wrong\n",
		a.Baseline.Allowed, a.Baseline.Count, a.Baseline.Errors)
}
