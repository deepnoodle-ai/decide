// Command fanout asks whether each retrieved passage is relevant to a
// query, one request per passage, with at most four requests in flight.
//
//	TYPESAFE_API_KEY=... go run ./examples/fanout
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/fanout"
)

const query = "How long should an access token live?"

var passages = []string{
	"Access tokens are short-lived; configure their expiry.",
	"Refresh tokens rotate when a session is refreshed.",
	"Sign-out revokes a session but an issued access token may remain valid.",
}

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	results, err := fanout.Map(context.Background(), client, passages, 4,
		func(_ context.Context, _ int, passage string) (*decide.Request, error) {
			req := decide.NewRequest(map[string]string{"query": query, "passage": passage})
			decide.Ask(req, "relevant", decide.Noul("Is this passage relevant to the query?"))
			return req, nil
		})
	if err != nil {
		log.Fatal(err)
	}

	// One passage failing does not lose the others.
	for i, r := range results {
		if r.Err != nil {
			fmt.Printf("error  %s: %v\n", passages[i], r.Err)
			continue
		}
		a, err := decide.AnswerAs[*decide.NoulAnswer](r.Response, "relevant")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%.2f  %s\n", a.Noul, passages[i])
	}
}
