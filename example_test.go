package decide_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

// startFake starts a fake API with canned answers, standing in for
// https://api.typesafe.ai so the examples run without a key.
func startFake() *decidetest.Server {
	srv, err := decidetest.Start()
	if err != nil {
		log.Fatal(err)
	}
	srv.Answer("billing", decidetest.NoulAnswer(0.93))
	return srv
}

// Ask a typed question and read its validated answer.
func Example() {
	srv := startFake()
	defer srv.Close()
	client, err := srv.NewClient() // in production: decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	req := decide.NewRequest("I was charged twice this month and nobody answers my emails.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing?"))

	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}
	answer, err := billing.From(resp)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("billing: %.2f\n", answer.Noul)
	// Output: billing: 0.93
}

// A validation failure returns the response and an error. Keys that passed
// stay usable; resp.Invalid names the ones that failed.
func ExampleClient_SystemOne_partialResults() {
	srv := startFake()
	defer srv.Close()
	// A deliberately inconsistent answer: "calm" is not the most likely option.
	srv.Answer("tone", &decide.ChoiceAnswer{
		Choice:        "calm",
		Probabilities: map[string]float64{"calm": 0.2, "angry": 0.8},
		Confidence:    0.6,
	})
	client, err := srv.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	req := decide.NewRequest("You people are useless.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing?"))
	tone := decide.Ask(req, "tone", decide.Choice("Tone?", decide.Option("calm"), decide.Option("angry")))

	resp, err := client.SystemOne(context.Background(), req)
	if resp == nil {
		log.Fatal(err)
	}
	fmt.Println("error:", err)
	for key, ae := range resp.Invalid {
		fmt.Println("invalid:", key, ae.Reason, errors.Is(ae, decide.ErrInconsistentAnswer))
	}
	if b, err := billing.From(resp); err == nil {
		fmt.Printf("billing still usable: %.2f\n", b.Noul)
	}
	t, err := tone.From(resp)
	fmt.Println("tone:", t.Choice, "err:", err != nil)
	// Output:
	// error: decide: 1 invalid answer: answer "tone" (choice): choice_not_argmax: choice "calm" has P=0.2, max is 0.8
	// invalid: tone choice_not_argmax true
	// billing still usable: 0.93
	// tone: calm err: true
}

// Rank a Choice answer's full distribution rather than only its top option.
func ExampleChoiceAnswer_Ranked() {
	a := &decide.ChoiceAnswer{
		Choice:        "refund",
		Probabilities: map[string]float64{"refund": 0.55, "exchange": 0.35, "other": 0.10},
	}
	for _, p := range a.Ranked() {
		fmt.Printf("%-8s %.2f\n", p.Key, p.P)
	}
	fmt.Printf("margin   %.2f\n", a.Margin())
	// Output:
	// refund   0.55
	// exchange 0.35
	// other    0.10
	// margin   0.20
}

// List the models the API advertises. Aliases such as jev-latest move; the
// resolved version is on every Response.
func ExampleModelsService_List() {
	srv := startFake()
	defer srv.Close()
	client, err := srv.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	list, err := client.Models.List(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range list.Models {
		fmt.Println(m.Name)
	}
	// Output:
	// jev-latest
	// jev-preview
}

// Branch on the kind of failure with errors.Is; every API error carries the
// request ID.
func ExampleAPIError() {
	srv := startFake()
	defer srv.Close()
	srv.Overloaded(3) // more than the default two retries
	client, err := srv.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	req := decide.NewRequest("state")
	decide.Ask(req, "billing", decide.Noul("About billing?"))

	_, err = client.SystemOne(context.Background(), req)
	if ae, ok := errors.AsType[*decide.APIError](err); ok {
		fmt.Println(ae.StatusCode, errors.Is(err, decide.ErrOverloaded), errors.Is(err, decide.ErrServer), ae.RequestID)
	}
	// Output:
	// 529 true true req_3
}

// Score is the expected zero-based level; Level selects the most likely level.
func ExampleScoreAnswer_Level() {
	a := decidetest.ScoreAnswer([]any{"can wait", "this week", "today"}, 0.1, 0.3, 0.6)
	fmt.Printf("expected level: %.2f\n", a.Score)
	fmt.Println("most likely:", a.LevelLabel(a.Level()))
	// Output:
	// expected level: 1.50
	// most likely: today
}
