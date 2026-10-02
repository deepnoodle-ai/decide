package pick_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/patterns/pick"
)

// Pick an original address from a candidate list. The fake
// server's numbers are placeholders, not model output.
func Example() {
	srv, err := decidetest.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()
	srv.Answer("receipt", decidetest.ChoiceAnswer(map[string]float64{
		"dana.whit@acme-corp.com": 0.01, "billing@acme-corp.com": 0.005, "orders@acme-corp.com": 0.0,
		"Dana.Personal@gmail.com": 0.98, "none": 0.005,
	}))
	client, err := srv.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	emails, err := pick.New([]string{"dana.whit@acme-corp.com", "billing@acme-corp.com", "orders@acme-corp.com", "Dana.Personal@gmail.com"})
	if err != nil {
		log.Fatal(err)
	}
	req := decide.NewRequest("Reply-To: Dana.Personal@gmail.com\n\nSend my receipt to my personal address.")
	receipt := emails.Ask(req, "receipt", "Which email address does the sender want their receipt sent to?")

	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}
	r, err := receipt.From(resp)
	if err != nil {
		log.Fatal(err)
	}
	if r.Picked {
		// r.Item is the caller's own string; normalize it in code.
		fmt.Println(strings.ToLower(r.Item), r.Index)
		fmt.Printf("p=%.2f margin=%.2f runner_up=%s\n", r.P, r.Margin, r.RunnerUp)
	}
	// Output:
	// dana.personal@gmail.com 3
	// p=0.98 margin=0.97 runner_up=dana.whit@acme-corp.com
}

// When the abstain option wins there is no item.
func ExamplePicker_Read() {
	phones, err := pick.New([]string{"(415) 555-0199", "(415) 555-0142"})
	if err != nil {
		log.Fatal(err)
	}
	a := &decide.ChoiceAnswer{
		Choice:        "none",
		Probabilities: map[string]float64{"(415) 555-0199": 0.2, "(415) 555-0142": 0.1, "none": 0.7},
		Confidence:    0.55,
	}
	r, err := phones.Read(a)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("picked=%v index=%d key=%s abstain_p=%.1f runner_up=%s\n", r.Picked, r.Index, r.Key, r.AbstainP, r.RunnerUp)
	// Output:
	// picked=false index=-1 key=none abstain_p=0.7 runner_up=(415) 555-0199
}

// Opaque positional keys, with each item's text sent as its description.
func ExampleKeyFunc() {
	rewrites, err := pick.New(
		[]string{"cheap flights to lisbon", "lisbon flights under $200"},
		pick.KeyFunc(func(i int, _ string) string { return fmt.Sprintf("r%d", i+1) }),
		pick.Abstain("keep_original", "None of these is better than the original query."),
	)
	if err != nil {
		log.Fatal(err)
	}
	b, err := rewrites.Question("Which rewrite best matches the user's intent?").MarshalJSON()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))
	// Output:
	// {"type":"choice","instructions":"Which rewrite best matches the user's intent?","criteria":{"r1":"cheap flights to lisbon","r2":"lisbon flights under $200","keep_original":"None of these is better than the original query."}}
}

// Walk the whole distribution, item by item, without a side map.
func ExamplePicker_Ranked() {
	amounts, err := pick.New([]string{"$1,200.00", "$1,315.50", "$50.00"})
	if err != nil {
		log.Fatal(err)
	}
	a := decidetest.ChoiceAnswer(map[string]float64{"$1,200.00": 0.15, "$1,315.50": 0.8, "$50.00": 0.0, "none": 0.05})
	for r, p := range amounts.Ranked(a) {
		fmt.Printf("%-10s picked=%-5v p=%.2f\n", r.Key, r.Picked, p)
	}
	// Output:
	// $1,315.50  picked=true  p=0.80
	// $1,200.00  picked=true  p=0.15
	// none       picked=false p=0.05
	// $50.00     picked=true  p=0.00
}
