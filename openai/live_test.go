//go:build live

// Live tests call the real Decisions API. Run with:
//
//	OPENAI_API_KEY=... go test -tags live -run Live ./openai
package openai_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/openai"
)

func TestLiveAllPrimitives(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}
	transport, err := openai.NewTransport(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	client, err := decide.NewClient(decide.WithoutEnvironment(), decide.WithTransport(transport), decide.WithModel("gpt-6-luna"))
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
	if resp.RequestID == "" || resp.Usage.InputTokens == 0 {
		t.Errorf("response %+v", resp)
	}
}
