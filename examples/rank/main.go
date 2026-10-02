// Command rank reranks a retrieved shortlist by asking, for each passage,
// whether it answers the query, then sorting by the answers.
//
//	TYPESAFE_API_KEY=... go run ./examples/rank
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/rank"
)

const query = "How do I recover an expired session?"

var shortlist = []string{
	"A general account of account access.",
	"The session recovery procedure: sign in again and resume from the saved draft.",
	"Refresh tokens rotate when a session is refreshed.",
}

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	var observations []rank.Observation
	for _, passage := range shortlist {
		req := decide.NewRequest(map[string]string{"query": query, "passage": passage})
		decide.Ask(req, "answers", decide.Noul("Does this passage directly answer the query?"))
		resp, err := client.SystemOne(context.Background(), req)
		if err != nil {
			log.Fatal(err)
		}
		o, err := rank.FromResponse(resp, "answers")
		if err != nil {
			log.Fatal(err)
		}
		observations = append(observations, o)
	}

	// Ties break on the passage text so the order is deterministic.
	order, err := rank.Rerank(shortlist, observations,
		func(_ int, passage string) string { return passage })
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range order.Entries {
		fmt.Printf("%.2f  %s\n", e.Value, e.Item)
	}
}
