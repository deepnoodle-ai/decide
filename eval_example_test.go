package decide_test

import (
	"context"
	"fmt"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func ExampleEval() {
	srv, err := decidetest.Start()
	if err != nil {
		panic(err)
	}
	defer srv.Close()
	client, err := srv.NewClient()
	if err != nil {
		panic(err)
	}
	srv.Answer("eval", decidetest.NoulAnswer(.9))
	e, err := decide.Eval(context.Background(), client, "Charged twice.", decide.Noul("Billing issue?"))
	if err != nil {
		panic(err)
	}
	fmt.Println(e.Answer.Noul, e.Response.Model)
	// Output: 0.9 jev-1.13.0
}

func ExamplePick() {
	srv, err := decidetest.Start()
	if err != nil {
		panic(err)
	}
	defer srv.Close()
	client, err := srv.NewClient()
	if err != nil {
		panic(err)
	}
	srv.Answer("pick", decidetest.ChoiceAnswer(map[string]float64{"c1": .9, "c2": .05, "none": .05}))
	d, err := decide.Pick(context.Background(), client, "Charged twice.", "Choose a queue.", []decide.Candidate[string]{
		{Item: "billing", Description: "Payments and invoices"},
		{Item: "engineering", Description: "Product defects"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(d.Item, d.Index, d.Picked)
	// Output: billing 0 true
}
