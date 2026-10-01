// Command heads triages a support ticket in one request: it asks which team
// owns the ticket and, speculatively, each team's follow-up question, then
// reads only the chosen team's answer.
//
//	TYPESAFE_API_KEY=... go run ./examples/heads
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/heads"
)

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	plan, err := heads.New(
		decide.Choice("Which team owns the main request?",
			decide.Option("bug", "A broken feature"),
			decide.Option("billing", "A payment or refund issue"),
			decide.Option("feature", "A request for new behavior")),
		heads.Branch{Option: "bug", Questions: map[string]decide.Question{
			"severity": decide.Score(
				"If this is a bug, how severe is the reported failure?",
				"Cosmetic", "Degraded", "Blocked"),
		}},
		heads.Branch{Option: "billing", Questions: map[string]decide.Question{
			"refund": decide.Noul(
				"If this is billing, does the customer request a refund?"),
		}},
		heads.Branch{Option: "feature"},
	)
	if err != nil {
		log.Fatal(err)
	}
	req := decide.NewRequest(
		"I was charged twice for order 98423. Please refund the extra charge.")
	binding, err := plan.Attach(req, "team")
	if err != nil {
		log.Fatal(err)
	}

	// An invalid answer to an unused branch should not fail the ticket, so
	// continue on ErrInvalidAnswer and let Read validate the chosen branch.
	resp, err := client.SystemOne(context.Background(), req)
	if err != nil && !errors.Is(err, decide.ErrInvalidAnswer) {
		log.Fatal(err)
	}
	r, err := binding.Read(resp)
	if err != nil {
		log.Fatal(err)
	}

	switch r.Branch {
	case "bug":
		a, err := heads.AnswerAs[*decide.ScoreAnswer](r, "severity")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("bug: severity %.2f\n", a.Score)
	case "billing":
		a, err := heads.AnswerAs[*decide.NoulAnswer](r, "refund")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("billing: refund requested %.2f\n", a.Noul)
	default:
		fmt.Println("feature: log the request")
	}
}
