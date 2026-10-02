// Command gate asks what file operation a user requested and gates the
// agent's proposed delete with patterns/gate: allow, review, or escalate.
//
//	TYPESAFE_API_KEY=... go run ./examples/gate
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// policy requires more confidence to delete than to read. Every threshold
// is a placeholder; measure your own on labeled traffic.
var policy = gate.Asymmetric{
	Input:   "action",
	Measure: gate.MeasureConfidence,
	Floor:   0.6,
	Options: map[string]gate.OptionBand{
		"read_file":   {Allow: 0.6, Review: 0.6},
		"delete_file": {Allow: 0.85, Review: 0.6},
		"none":        {Always: gate.Escalate},
	},
}

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	req := decide.NewRequest(
		`User: "Can you clean up the drafts?" Agent proposes: delete_file(path="drafts/final.txt")`)
	decide.Ask(req, "action", decide.Choice(
		"Which file operation did the user ask the agent to perform?",
		decide.Option("read_file", "Read or show the contents of a file"),
		decide.Option("delete_file", "Delete a file"),
		decide.Option("none", "Neither, or the proposed call does not match the request"),
	))

	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		log.Print(err) // a nil response gates as missing, which escalates
	}
	d := policy.Evaluate(gate.FromResponse(resp))
	fmt.Printf("%s: %s\n", d.Outcome, d.Reason)
}
