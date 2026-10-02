// Command funnel picks tools for a request in two stages: skim every tool's
// one-line summary in one request, then re-check the two best with their
// full descriptions, one request each.
//
//	TYPESAFE_API_KEY=... go run ./examples/funnel
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/funnel"
)

const request = "Move my 3pm with Sam to Friday and let him know."

type tool struct{ name, summary, full string }

var tools = []tool{
	{"calendar", "Calendar events", "Create, move, and cancel calendar events and invitations."},
	{"email", "Send email", "Compose and send email from the user's account."},
	{"weather", "Weather forecasts", "Look up current weather and forecasts by city."},
	{"notes", "Personal notes", "Search and edit the user's plain-text notes."},
}

func fits(it funnel.Item[tool], stage string) float64 {
	a, err := funnel.AnswerAs[*decide.NoulAnswer](it, stage, "fits")
	if err != nil {
		log.Fatal(err)
	}
	return a.Noul
}

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	// The thresholds are illustrative. Measure your own before relying on them.
	res, err := funnel.Run(context.Background(), client, tools, 4,
		funnel.Stage[tool]{
			Name: "skim",
			Ask: funnel.Shared(request, func(it funnel.Item[tool]) (map[string]decide.Question, error) {
				return map[string]decide.Question{"fits": decide.Noul(
					"Is the tool '" + it.Value.name + ": " + it.Value.summary + "' needed for this request?")}, nil
			}, 0),
			Keep:  func(it funnel.Item[tool]) (bool, error) { return fits(it, "skim") >= 0.3, nil },
			Rank:  func(it funnel.Item[tool]) (float64, error) { return fits(it, "skim"), nil },
			Limit: 2,
		},
		funnel.Stage[tool]{
			Name: "check",
			Ask: funnel.PerItem(func(_ context.Context, it funnel.Item[tool]) (*decide.Request, error) {
				req := decide.NewRequest(map[string]string{"request": request, "tool": it.Value.full})
				req.Questions["fits"] = decide.Noul("Is this tool needed to complete the request?")
				return req, nil
			}),
			Keep: func(it funnel.Item[tool]) (bool, error) { return fits(it, "check") >= 0.5, nil },
		})
	if err != nil {
		log.Fatal(err)
	}

	for _, it := range res.Items {
		if it.Drop != nil {
			fmt.Printf("%-8s dropped at %s (%s) %v\n", it.Value.name, it.Drop.Stage, it.Drop.Cause, it.Drop.Err)
			continue
		}
		fmt.Printf("%-8s kept: skim %.2f, check %.2f\n", it.Value.name, fits(it, "skim"), fits(it, "check"))
	}
	for _, r := range res.Stages {
		fmt.Printf("stage %-5s in %d, out %d, failed %d, %d input tokens\n", r.Name, r.In, r.Out, r.Failed, r.InputTokens)
	}
}
