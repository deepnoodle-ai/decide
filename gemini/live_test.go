//go:build live

// Live tests call the real Gemini Decisions API. Run with:
//
//	GEMINI_API_KEY=... go test -tags live -run Live ./gemini
package gemini_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/gemini"
)

func TestLiveAllPrimitives(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY is not set")
	}
	transport, err := gemini.NewTransport(gemini.Config{APIKey: os.Getenv("GEMINI_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithTransport(transport), decide.WithModel("gemini-decision-1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	req := decide.NewRequest("Help! My payouts have been failing for 3 days.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing or payouts?"))
	tone := decide.Ask(req, "tone", decide.Choice("What is the customer's tone?",
		decide.Option("calm"), decide.Option("frustrated"), decide.Option("angry")))
	urgency := decide.Ask(req, "urgency", decide.Score("How urgent is this?", "can wait", "this week", "today"))
	resp, err := client.SystemOne(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := billing.From(resp); err != nil || b.Noul < 0.5 {
		t.Errorf("billing %+v %v", b, err)
	}
	if c, err := tone.From(resp); err != nil || c.Choice == "calm" {
		t.Errorf("tone %+v %v", c, err)
	}
	if s, err := urgency.From(resp); err != nil || s.Score < 1 || s.Legend["2"] != "today" {
		t.Errorf("urgency %+v %v", s, err)
	}
	if resp.Usage.InputTokens == 0 {
		t.Errorf("response %+v", resp)
	}
}
