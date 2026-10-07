package templates_test

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/templates"
)

// Ask command-risk's severe question about a command, and flag the answer
// as decide run would.
func Example() {
	srv, err := decidetest.Start() // a fake API; in production, decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()
	srv.Answer("severe", decidetest.NoulAnswer(0.91))
	client, err := srv.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	req := decide.NewRequest("git push --force origin main")
	severe := decide.Ask(req, "severe", templates.CommandRisk.Noul("severe"))
	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}
	a, err := severe.From(resp)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("severe: %.2f, flag %q, flagged %v\n",
		a.Noul, templates.CommandRisk.Flag("severe"), templates.CommandRisk.Flagged("severe", a))
	// Output: severe: 0.91, flag ["yes >= 80%"], flagged true
}

// Set a parameter that has no default before asking the question.
func ExampleTemplate_With() {
	relevance, err := templates.Relevance.With(map[string]string{"question": "database migrations"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(relevance.Parameters())
	_, err = templates.Relevance.With(nil)
	fmt.Println(err)
	// Output:
	// [question]
	// relevance needs a value for "question"
}
