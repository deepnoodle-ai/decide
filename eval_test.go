package decide_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func TestEvalTypedAnswersAndEvidence(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("eval", decidetest.NoulAnswer(.73))
	e, err := decide.Eval(context.Background(), srv.Client(t), "ticket", decide.Noul("Billing?"), decide.WithRequestModel("requested"))
	if err != nil || e.Answer == nil || e.Answer.Noul != .73 || e.Response == nil || e.Response.RequestID == "" {
		t.Fatalf("evaluation: %+v, %v", e, err)
	}
	requests := srv.Requests()
	if len(requests) != 1 || requests[0].Request.Model != "requested" || len(requests[0].Request.Questions) != 1 {
		t.Fatalf("requests: %+v", requests)
	}
}

func TestPickOriginalItemAndAbstention(t *testing.T) {
	type item struct{ Name string }
	original := &item{"engineering"}
	candidates := []decide.Candidate[*item]{{Item: &item{"billing"}, Description: "payments"}, {Item: original, Description: "defects"}}
	for _, abstain := range []bool{false, true} {
		t.Run(map[bool]string{false: "picked", true: "abstained"}[abstain], func(t *testing.T) {
			srv := decidetest.NewServer(t)
			probs := map[string]float64{"c1": .1, "c2": .8, "none": .1}
			if abstain {
				probs = map[string]float64{"c1": .1, "c2": .1, "none": .8}
			}
			srv.Answer("pick", decidetest.ChoiceAnswer(probs))
			d, err := decide.Pick(context.Background(), srv.Client(t), "ticket", "Select a queue.", candidates)
			if err != nil || d.Answer == nil || d.Response == nil || d.Picked == abstain {
				t.Fatalf("decision: %+v, %v", d, err)
			}
			if !abstain && (d.Item != original || d.Index != 1) {
				t.Fatalf("original item lost: %+v", d)
			}
			if abstain && (d.Item != nil || d.Index != -1) {
				t.Fatalf("abstention: %+v", d)
			}
		})
	}
}

func TestPickValidationCannotAuthorizeMixedFailures(t *testing.T) {
	for _, tc := range []struct {
		name            string
		answer          *decide.ChoiceAnswer
		extra           bool
		wantPicked      bool
		wantConsistency bool
	}{
		{"valid", decidetest.ChoiceAnswer(map[string]float64{"c1": .9, "none": .1}), false, true, false},
		{"inconsistent", &decide.ChoiceAnswer{Choice: "c1", Probabilities: map[string]float64{"c1": .1, "none": .9}, Confidence: .8}, false, true, true},
		{"mixed", &decide.ChoiceAnswer{Choice: "c1", Probabilities: map[string]float64{"c1": .1, "none": .9}, Confidence: .8}, true, false, true},
		{"unmapped", &decide.ChoiceAnswer{Choice: "other", Probabilities: map[string]float64{"c1": .9, "none": .1}, Confidence: .8}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := decidetest.NewServer(t)
			srv.Respond(func(_ *decide.Request) (*decide.Response, error) {
				answers := map[string]decide.Answer{"pick": tc.answer}
				if tc.extra {
					answers["unexpected"] = decidetest.NoulAnswer(.5)
				}
				return &decide.Response{Model: "resolved", Answers: answers}, nil
			})
			d, err := decide.Pick(context.Background(), srv.Client(t, decide.WithValidation(false)), "state", "Choose", []decide.Candidate[string]{{Item: "original", Description: "value"}})
			if d.Picked != tc.wantPicked || d.Response == nil || errors.Is(err, decide.ErrInconsistentAnswer) != tc.wantConsistency {
				t.Fatalf("decision: %+v, error %v", d, err)
			}
			if tc.name != "valid" && err == nil {
				t.Fatal("invalid answer succeeded")
			}
			if !tc.wantPicked && (d.Item != "" || d.Index != -1 || d.Answer != nil) {
				t.Fatalf("structural failure leaked selection: %+v", d)
			}
		})
	}
}

func TestConveniencePreflightAndCancellation(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	d, err := decide.Pick(context.Background(), client, "state", "choose", []decide.Candidate[string]{})
	if err != nil || d.Picked || d.Index != -1 || d.Answer != nil || d.Response != nil {
		t.Fatalf("empty: %+v %v", d, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decide.Pick(ctx, client, "state", "choose", []decide.Candidate[string]{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := decide.Eval(ctx, client, "state", decide.Noul("question")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	var nilQ *decide.NoulQuestion
	for _, q := range []*decide.NoulQuestion{nil, nilQ} {
		if _, err := decide.Eval(context.Background(), client, "state", q); !errors.Is(err, decide.ErrInvalidRequest) {
			t.Fatalf("nil question: %v", err)
		}
	}
	if _, err := decide.Eval(nil, client, "state", decide.Noul("q")); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("nil context: %v", err)
	}
	if _, err := decide.Eval(context.Background(), nil, "state", decide.Noul("q")); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("nil client: %v", err)
	}
	if _, err := decide.Pick(context.Background(), client, "state", "choose", make([]decide.Candidate[string], 255)); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("candidate cap: %v", err)
	}
	if _, err := decide.Eval(context.Background(), client, "state", decide.Score("q", "only one level")); !errors.Is(err, decide.ErrInvalidRequest) {
		t.Fatalf("score request validation: %v", err)
	}
	if len(srv.Requests()) != 0 {
		t.Fatal("preflight called the network")
	}
}

// An external question supplies NewAnswer and validation, but neither a
// registry entry nor AnswerMaker. Eval must infer the constructor itself.
type localQuestion struct{ reject bool }

func (localQuestion) QuestionType() string    { return "local-eval-test" }
func (localQuestion) NewAnswer() *localAnswer { return &localAnswer{} }
func (localQuestion) MarshalJSON() ([]byte, error) {
	return []byte(`{"type":"local-eval-test","instructions":"q"}`), nil
}
func (q localQuestion) ValidateAnswer(a decide.Answer) error {
	if q.reject {
		return errors.New("custom rejection")
	}
	if _, ok := a.(*localAnswer); !ok {
		return errors.New("wrong answer type")
	}
	return nil
}

type localAnswer struct {
	Number int `json:"number"`
}

func (*localAnswer) AnswerType() string { return "local-eval-test" }
func (a *localAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"type": a.AnswerType(), "number": a.Number})
}
func (a *localAnswer) UnmarshalJSON(b []byte) error {
	type alias localAnswer
	return json.Unmarshal(b, (*alias)(a))
}

func TestEvalExternalQuestionWithoutRegistration(t *testing.T) {
	for _, reject := range []bool{false, true} {
		srv := decidetest.NewServer(t)
		srv.Respond(func(_ *decide.Request) (*decide.Response, error) {
			return &decide.Response{Model: "resolved", Answers: map[string]decide.Answer{"eval": &localAnswer{Number: 42}}}, nil
		})
		e, err := decide.Eval(context.Background(), srv.Client(t), "state", localQuestion{reject: reject})
		if e.Response == nil {
			t.Fatal("missing evidence")
		}
		if reject {
			if !errors.Is(err, decide.ErrInvalidAnswer) || e.Answer != nil {
				t.Fatalf("custom validator: %+v %v", e, err)
			}
		} else if err != nil || e.Answer == nil || e.Answer.Number != 42 {
			t.Fatalf("custom answer: %+v %v", e, err)
		}
	}
}

func TestEvalFailuresRetainOnlyClientEvidence(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t, decide.WithMaxRetries(0), decide.WithValidation(false))
	srv.Respond(func(_ *decide.Request) (*decide.Response, error) {
		return &decide.Response{Answers: map[string]decide.Answer{"eval": decidetest.NoulAnswer(2)}}, nil
	})
	e, err := decide.Eval(context.Background(), client, "state", decide.Noul("q"))
	if !errors.Is(err, decide.ErrInvalidAnswer) || e.Answer != nil || e.Response == nil {
		t.Fatalf("structural evidence: %+v, %v", e, err)
	}
	srv.FailNext(401)
	e, err = decide.Eval(context.Background(), client, "state", decide.Noul("q"))
	if !errors.Is(err, decide.ErrAuth) || e.Answer != nil || e.Response != nil {
		t.Fatalf("transport evidence: %+v, %v", e, err)
	}
}
