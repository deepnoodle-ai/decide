// Command compact trims an agent transcript before the next turn: it asks
// whether each tool call is still needed for the task, then keeps the most
// needed ones verbatim under a budget, using a short form where the full
// output is not needed.
//
//	TYPESAFE_API_KEY=... go run ./examples/compact
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/compact"
)

var transcript = []compact.Segment{
	{Text: "user: Fix the failing parser test. Don't change exported signatures.", Pinned: true},
	{Text: "tool ls: README.md go.mod parser/ cmd/ docs/", Short: "listed the repo root"},
	{Text: "tool go test ./parser: FAIL TestParseNestedCall: unexpected \")\" at offset 16",
		Short: "parser tests: TestParseNestedCall fails"},
	{Text: "tool read README.md: Exprkit parses and evaluates expressions in config files.",
		Short: "read README.md"},
	{Text: "tool read parser/call.go: parseArgs consumes ')' inside its loop, then loops again.",
		Short: "read parser/call.go"},
}

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	for i := range transcript { // sizes in bytes here; use your tokenizer's counts
		transcript[i].Size, transcript[i].ShortSize = len(transcript[i].Text), len(transcript[i].Short)
	}

	// The cutoffs are illustrative. Measure your own before relying on them.
	res, err := compact.Compact(context.Background(), client, transcript, compact.Config{
		Focus:   "Next step: make the fix in the parser and rerun the parser tests.",
		Rule:    compact.Rule{Budget: 220, Floor: 0.2, ShortBelow: 0.5},
		Workers: 2,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range res.Decisions {
		fmt.Printf("%-5s %-8s needed %.2f  %.40s\n", d.Action, d.Cause, d.Scores.Needed, transcript[d.Index].Text)
	}
	fmt.Printf("%d -> %d bytes, %d request(s)\n", res.Before, res.After, res.Requests)
}
