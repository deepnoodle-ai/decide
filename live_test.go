//go:build live

// Live tests call the real API. Run with:
//
//	TYPESAFE_API_KEY=... go test -tags live -run Live ./...
package decide_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
)

func liveClient(t *testing.T, opts ...decide.ClientOption) *decide.Client {
	t.Helper()
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}
	c, err := decide.NewClient(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func liveCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

var versioned = regexp.MustCompile(`^jev-\d+\.\d+\.\d+$`)

func TestLiveAllPrimitives(t *testing.T) {
	c := liveClient(t)
	req := decide.NewRequest("Help! My payouts have been failing for 3 days.")
	billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing or payouts?"))
	tone := decide.Ask(req, "tone", decide.Choice("What is the customer's tone?",
		decide.Option("calm"), decide.Option("frustrated"), decide.Option("angry")))
	urgency := decide.Ask(req, "urgency", decide.Score("How urgent is this?", "can wait", "this week", "today"))
	resp, err := c.SystemOne(liveCtx(t), req)
	if err != nil {
		t.Fatalf("%v (request_id=%s)", err, requestID(resp))
	}
	if !versioned.MatchString(resp.Model) {
		t.Errorf("resolved model %q is not a versioned ID", resp.Model)
	}
	if _, err := billing.From(resp); err != nil {
		t.Error(err)
	}
	if _, err := tone.From(resp); err != nil {
		t.Error(err)
	}
	if _, err := urgency.From(resp); err != nil {
		t.Error(err)
	}
	if resp.RequestID == "" {
		t.Error("no request ID")
	}
}

func TestLiveModelsList(t *testing.T) {
	list, err := liveClient(t).Models.List(liveCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Models) == 0 {
		t.Fatal("no models")
	}
}

func TestLiveBadKey(t *testing.T) {
	liveClient(t) // skip without a key
	c, err := decide.NewClient(decide.WithAPIKey("sk-invalid-live-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Models.List(liveCtx(t))
	ae, ok := errors.AsType[*decide.APIError](err)
	if !ok || !errors.Is(err, decide.ErrAuth) || ae.RequestID == "" {
		t.Fatalf("err = %v", err)
	}
}

// TestLiveValidationCorpus sends a fixed corpus and asserts zero validation
// failures of any kind. It is the evidence required before tightening the
// provisional tolerances in validate.go.
func TestLiveValidationCorpus(t *testing.T) {
	c := liveClient(t)
	raw, err := os.ReadFile("testdata/live_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		States    []json.RawMessage          `json:"states"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Questions) < 20 {
		t.Fatalf("corpus has %d questions, want at least 20", len(corpus.Questions))
	}
	for i, state := range corpus.States {
		req := decide.NewRequest(state)
		var s string
		if json.Unmarshal(state, &s) == nil {
			req.State = s
		}
		for k, qraw := range corpus.Questions {
			q, err := decide.DecodeQuestion(qraw)
			if err != nil {
				t.Fatalf("question %s: %v", k, err)
			}
			req.Questions[k] = q
		}
		resp, err := c.SystemOne(liveCtx(t), req)
		if resp != nil {
			for k, ae := range resp.Invalid {
				t.Errorf("state %d, %s: %v (request_id=%s)", i, k, ae, resp.RequestID)
			}
		}
		if err != nil && (resp == nil || len(resp.Invalid) == 0) {
			t.Errorf("state %d: %v", i, err)
		}
	}
}

func requestID(resp *decide.Response) string {
	if resp == nil {
		return ""
	}
	return resp.RequestID
}
