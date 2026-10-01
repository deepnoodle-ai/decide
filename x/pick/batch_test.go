package pick_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/pick"
)

// An email with several candidate receipt addresses.
const emailDoc = `From: Dana Whit <dana.whit@acme-corp.com>
To: billing@acme-corp.com
Cc: orders@acme-corp.com
Reply-To: dana.personal@gmail.com

Hi team - please don't use the billing alias for this one. Send my receipt to my
personal address instead. Thanks, Dana.`

var (
	emailItems  = []string{"dana.whit@acme-corp.com", "billing@acme-corp.com", "orders@acme-corp.com", "dana.personal@gmail.com"}
	amountItems = []string{"$1,200.00", "$115.50", "$1,315.50", "$50.00"}
)

// batch is section 4's request: three picks and a Noul.
type batch struct {
	req                    *decide.Request
	receipt, sender, total pick.Handle[string]
	urgent                 decide.Handle[*decide.NoulAnswer]
}

func newBatch(t *testing.T) batch {
	t.Helper()
	emails, err := pick.New(emailItems)
	if err != nil {
		t.Fatal(err)
	}
	amounts, err := pick.New(amountItems)
	if err != nil {
		t.Fatal(err)
	}
	req := decide.NewRequest(emailDoc)
	return batch{
		req:     req,
		receipt: emails.Ask(req, "receipt", "Which email address does the sender want their receipt sent to?"),
		sender:  emails.Ask(req, "sender", "Which email address did this message come from (the From line)?"),
		total:   amounts.Ask(req, "total", "Which amount is the total the customer must pay?"),
		urgent:  decide.Ask(req, "urgent", decide.Noul("Does the sender say this is urgent?")),
	}
}

// Placeholder distributions, not model output.
func cannedBatch(srv *decidetest.Server) {
	srv.Answer("receipt", decidetest.ChoiceAnswer(map[string]float64{
		"dana.whit@acme-corp.com": 0.005, "billing@acme-corp.com": 0.005, "orders@acme-corp.com": 0.0,
		"dana.personal@gmail.com": 0.98, "none": 0.01,
	}))
	srv.Answer("sender", decidetest.ChoiceAnswer(map[string]float64{
		"dana.whit@acme-corp.com": 0.99, "billing@acme-corp.com": 0.0, "orders@acme-corp.com": 0.0,
		"dana.personal@gmail.com": 0.01, "none": 0.0,
	}))
	srv.Answer("total", decidetest.ChoiceAnswer(map[string]float64{
		"$1,200.00": 0.02, "$115.50": 0.0, "$1,315.50": 0.97, "$50.00": 0.0, "none": 0.01,
	}))
	srv.Answer("urgent", decidetest.NoulAnswer(0.03))
}

var emailProbs = map[string]float64{
	"dana.whit@acme-corp.com": 0.05, "billing@acme-corp.com": 0.1, "orders@acme-corp.com": 0.0,
	"dana.personal@gmail.com": 0.8, "none": 0.05,
}

func TestBatch(t *testing.T) {
	tests := []struct {
		name  string
		setup func(srv *decidetest.Server)
		check func(t *testing.T, b batch, resp *decide.Response, sendErr error)
	}{
		{
			name: "three picks and a noul in one request",
			check: func(t *testing.T, b batch, resp *decide.Response, sendErr error) {
				if sendErr != nil {
					t.Fatal(sendErr)
				}
				for _, c := range []struct {
					h     pick.Handle[string]
					item  string
					index int
				}{
					{b.receipt, "dana.personal@gmail.com", 3},
					{b.sender, "dana.whit@acme-corp.com", 0},
					{b.total, "$1,315.50", 2},
				} {
					r, err := c.h.From(resp)
					if err != nil {
						t.Fatalf("%s: %v", c.h.Key(), err)
					}
					if !r.Picked || r.Item != c.item || r.Index != c.index || r.Key != c.item {
						t.Errorf("%s: got %+v, want %q at %d", c.h.Key(), r, c.item, c.index)
					}
				}
				if u, err := b.urgent.From(resp); err != nil || u.Noul != 0.03 {
					t.Errorf("urgent = %v, %v", u, err)
				}
			},
		},
		{
			name: "choice_not_option is returned by From",
			setup: func(srv *decidetest.Server) {
				srv.Answer("receipt", &decide.ChoiceAnswer{Choice: "someone@else.com", Probabilities: emailProbs, Confidence: 0.7})
			},
			check: func(t *testing.T, b batch, resp *decide.Response, sendErr error) {
				if !errors.Is(sendErr, decide.ErrInvalidAnswer) {
					t.Fatalf("SystemOne err = %v, want ErrInvalidAnswer", sendErr)
				}
				_, err := b.receipt.From(resp)
				ae, ok := errors.AsType[*decide.AnswerError](err)
				if !ok || ae != resp.Invalid["receipt"] || ae.Reason != decide.ReasonChoiceNotOption {
					t.Fatalf("From err = %v, want resp.Invalid[receipt] (choice_not_option)", err)
				}
				if errors.Is(err, pick.ErrUnmappedChoice) || errors.Is(err, decide.ErrInconsistentAnswer) {
					t.Error("client error was replaced by pick's")
				}
				if r, err := b.sender.From(resp); err != nil || r.Item != "dana.whit@acme-corp.com" {
					t.Errorf("sender = %+v, %v; other keys must stay usable", r, err)
				}
			},
		},
		{
			name: "argmax mismatch returns the full result and the consistency error",
			setup: func(srv *decidetest.Server) {
				srv.Answer("receipt", &decide.ChoiceAnswer{Choice: "billing@acme-corp.com", Probabilities: emailProbs, Confidence: 0.7})
			},
			check: func(t *testing.T, b batch, resp *decide.Response, sendErr error) {
				if !errors.Is(resp.Invalid["receipt"], decide.ErrInconsistentAnswer) {
					t.Fatalf("Invalid[receipt] = %v, want ErrInconsistentAnswer", resp.Invalid["receipt"])
				}
				r, err := b.receipt.From(resp)
				if !errors.Is(err, decide.ErrInconsistentAnswer) || err != error(resp.Invalid["receipt"]) {
					t.Fatalf("err = %v, want resp.Invalid[receipt]", err)
				}
				if !r.Picked || r.Item != "billing@acme-corp.com" || r.P != 0.1 || r.RunnerUp != "dana.personal@gmail.com" {
					t.Errorf("got %+v", r)
				}
				if r.Margin >= 0 || r.Margin != 0.1-emailProbs["dana.personal@gmail.com"] {
					t.Errorf("Margin = %v, want 0.1-0.8", r.Margin)
				}
			},
		},
		{
			name: "probability sum off returns the full result and the consistency error",
			setup: func(srv *decidetest.Server) {
				srv.Answer("total", &decide.ChoiceAnswer{Choice: "$1,315.50", Confidence: 0.5, Probabilities: map[string]float64{
					"$1,200.00": 0.1, "$115.50": 0.0, "$1,315.50": 0.4, "$50.00": 0.0, "none": 0.0,
				}})
			},
			check: func(t *testing.T, b batch, resp *decide.Response, sendErr error) {
				if ae := resp.Invalid["total"]; ae == nil || ae.Reason != decide.ReasonProbabilitySum {
					t.Fatalf("Invalid[total] = %v, want probability_sum", ae)
				}
				r, err := b.total.From(resp)
				if !errors.Is(err, decide.ErrInconsistentAnswer) || !r.Picked || r.Item != "$1,315.50" || r.Index != 2 || r.P != 0.4 {
					t.Errorf("got %+v, %v", r, err)
				}
			},
		},
		{
			name:  "type mismatch passes through",
			setup: func(srv *decidetest.Server) { srv.Answer("total", decidetest.NoulAnswer(0.5)) },
			check: func(t *testing.T, b batch, resp *decide.Response, sendErr error) {
				_, err := b.total.From(resp)
				if ae, ok := errors.AsType[*decide.AnswerError](err); !ok || ae.Reason != decide.ReasonTypeMismatch {
					t.Fatalf("err = %v, want type_mismatch", err)
				}
			},
		},
		{
			name: "missing answer passes through",
			setup: func(srv *decidetest.Server) {
				srv.Respond(func(req *decide.Request) (*decide.Response, error) {
					return &decide.Response{Answers: map[string]decide.Answer{"urgent": decidetest.NoulAnswer(0.1)}}, nil
				})
			},
			check: func(t *testing.T, b batch, resp *decide.Response, sendErr error) {
				_, err := b.receipt.From(resp)
				if ae, ok := errors.AsType[*decide.AnswerError](err); !ok || ae.Reason != decide.ReasonMissingAnswer {
					t.Fatalf("err = %v, want missing_answer", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := decidetest.NewServer(t)
			cannedBatch(srv)
			if tt.setup != nil {
				tt.setup(srv)
			}
			b := newBatch(t)
			resp, err := srv.Client(t).SystemOne(t.Context(), b.req)
			if resp == nil {
				t.Fatalf("no response: %v", err)
			}
			if n := len(srv.Requests()); n != 1 {
				t.Fatalf("recorded %d requests, want 1", n)
			}
			if got, want := len(srv.Requests()[0].Request.Questions), 4; got != want {
				t.Fatalf("request has %d questions, want %d", got, want)
			}
			tt.check(t, b, resp, err)
		})
	}
}

func TestFromNilResponse(t *testing.T) {
	b := newBatch(t)
	if _, err := b.receipt.From(nil); !errors.Is(err, decide.ErrInvalidAnswer) {
		t.Fatalf("err = %v, want ErrInvalidAnswer", err)
	}
}

func TestRanked(t *testing.T) {
	p, err := pick.New([]string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	a := answer(map[string]float64{"c": 0.4, "none": 0.2, "a": 0.2, "b": 0.2}, "")
	type row struct {
		key, runnerUp string
		picked        bool
		index         int
		p             float64
	}
	var got []row
	for r, prob := range p.Ranked(a) {
		if r.P != prob || r.Answer != a || r.AbstainP != 0.2 {
			t.Errorf("%s: P=%v prob=%v AbstainP=%v", r.Key, r.P, prob, r.AbstainP)
		}
		got = append(got, row{r.Key, r.RunnerUp, r.Picked, r.Index, prob})
	}
	want := []row{
		{"c", "a", true, 2, 0.4}, // runner-up: ties by key, so "a"
		{"a", "c", true, 0, 0.2}, // ties by key ascending: a, b, none
		{"b", "c", true, 1, 0.2},
		{"none", "c", false, -1, 0.2},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	n := 0
	for range p.Ranked(a) {
		n++
		break
	}
	if n != 1 {
		t.Error("Ranked did not stop on break")
	}
	for range p.Ranked(nil) {
		t.Error("Ranked(nil) yielded")
	}
	// Ranked and ChoiceAnswer.Ranked agree on order.
	var keys []string
	for r := range p.Ranked(a) {
		keys = append(keys, r.Key)
	}
	var clientKeys []string
	for _, e := range a.Ranked() {
		clientKeys = append(clientKeys, e.Key)
	}
	if !slices.Equal(keys, clientKeys) {
		t.Errorf("order %q, client order %q", keys, clientKeys)
	}
}

func TestAll(t *testing.T) {
	p, err := pick.New([]string{"b", "none", "a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	var keys, got []string
	for k, item := range p.All() {
		keys = append(keys, k)
		got = append(got, item)
	}
	if want := []string{"b", "none (2)", "a", "b (2)"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
	if want := []string{"b", "none", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("items = %q, want %q", got, want)
	}
	n := 0
	for range p.All() {
		n++
		break
	}
	if n != 1 {
		t.Error("All did not stop on break")
	}
}
