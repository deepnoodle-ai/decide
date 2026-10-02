// Command pick finds candidate email addresses with a regex and asks the
// model which one the sender wants their receipt sent to. The answer is the
// verbatim address the regex found, never generated text.
//
//	TYPESAFE_API_KEY=... go run ./examples/pick
package main

import (
	"context"
	"fmt"
	"log"
	"regexp"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/pick"
)

const email = `From: Dana Whit <dana.whit@acme-corp.com>
To: billing@acme-corp.com
Cc: orders@acme-corp.com
Reply-To: dana.personal@gmail.com

Hi team - please don't use the billing alias for this one. Send my receipt to my
personal address instead. Thanks, Dana.`

var emailRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

func main() {
	client, err := decide.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	addresses, err := pick.New(emailRE.FindAllString(email, -1))
	if err != nil {
		log.Fatal(err)
	}
	req := decide.NewRequest(email)
	receipt := addresses.Ask(req, "receipt",
		"Which email address does the sender want their receipt sent to?")

	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}
	r, err := receipt.From(resp)
	if err != nil {
		log.Fatal(err)
	}
	if !r.Picked {
		fmt.Printf("no pick (abstain p=%.2f)\n", r.AbstainP)
		return
	}
	fmt.Printf("selected %s (p=%.2f)\n", r.Item, r.P)
}
