package decide_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

var (
	noulQ   = decide.Noul("q")
	choiceQ = decide.Choice("q", decide.Option("a"), decide.Option("b"))
	scoreQ  = decide.Score("q", "lo", "mid", "hi")
)

func goodChoice() *decide.ChoiceAnswer {
	return &decide.ChoiceAnswer{Choice: "a", Probabilities: map[string]float64{"a": 0.7, "b": 0.3}, Confidence: 0.4}
}

func goodScore() *decide.ScoreAnswer {
	return decidetest.ScoreAnswer([]any{"lo", "mid", "hi"}, 0.2, 0.5, 0.3)
}

// fakeChoice claims to be a choice answer but is not *ChoiceAnswer.
type fakeChoice struct{}

func (fakeChoice) AnswerType() string { return "choice" }

// validateOne sends q under "k" and returns the error for that key.
func validateOne(t *testing.T, q decide.Question, a decide.Answer) *decide.AnswerError {
	t.Helper()
	req := decide.NewRequest("s")
	req.Questions["k"] = q
	resp, err := stubClient(t, answering(map[string]decide.Answer{"k": a})).SystemOne(t.Context(), req)
	if err == nil {
		if resp.Invalid != nil {
			t.Fatalf("Invalid set without error: %v", resp.Invalid)
		}
		return nil
	}
	if resp == nil {
		t.Fatalf("validation failure returned nil response: %v", err)
	}
	if len(resp.Invalid) != 1 {
		t.Fatalf("want one invalid key, got %v", resp.Invalid)
	}
	return resp.Invalid["k"]
}

func TestValidationChecks(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	withChoice := func(f func(*decide.ChoiceAnswer)) *decide.ChoiceAnswer { a := goodChoice(); f(a); return a }
	withScore := func(f func(*decide.ScoreAnswer)) *decide.ScoreAnswer { a := goodScore(); f(a); return a }
	cases := []struct {
		name       string
		q          decide.Question
		a          decide.Answer
		reason     string
		consistent bool // true when the failure is a consistency check
	}{
		{"raw decode error", choiceQ, &decide.RawAnswer{Type: "choice", Err: errors.New("bad")}, decide.ReasonDecodeFailed, false},
		{"4 type mismatch", choiceQ, decidetest.NoulAnswer(0.5), decide.ReasonTypeMismatch, false},
		{"3 concrete type", choiceQ, fakeChoice{}, decide.ReasonDecodeFailed, false},
		{"5 noul NaN", noulQ, decidetest.NoulAnswer(nan), decide.ReasonNotFinite, false},
		{"6 noul range", noulQ, decidetest.NoulAnswer(1.5), decide.ReasonOutOfRange, false},
		{"7 confidence Inf", choiceQ, withChoice(func(a *decide.ChoiceAnswer) { a.Confidence = inf }), decide.ReasonNotFinite, false},
		{"7 probability NaN", scoreQ, withScore(func(a *decide.ScoreAnswer) { a.Probabilities["1"] = nan }), decide.ReasonNotFinite, false},
		{"8 probability range", choiceQ, withChoice(func(a *decide.ChoiceAnswer) { a.Probabilities["b"] = -0.1 }), decide.ReasonOutOfRange, false},
		{"8 confidence range", scoreQ, withScore(func(a *decide.ScoreAnswer) { a.Confidence = 1.2 }), decide.ReasonOutOfRange, false},
		{"9 choice probability keys", choiceQ, withChoice(func(a *decide.ChoiceAnswer) { a.Probabilities["c"] = 0 }), decide.ReasonProbabilityKeys, false},
		{"10 choice not option", choiceQ, withChoice(func(a *decide.ChoiceAnswer) { a.Choice = "z" }), decide.ReasonChoiceNotOption, false},
		{"11 choice not argmax", choiceQ, withChoice(func(a *decide.ChoiceAnswer) { a.Choice = "b" }), decide.ReasonChoiceNotArgmax, true},
		{"12 score probability keys", scoreQ, withScore(func(a *decide.ScoreAnswer) { delete(a.Probabilities, "2") }), decide.ReasonProbabilityKeys, false},
		{"13 legend keys", scoreQ, withScore(func(a *decide.ScoreAnswer) { a.Legend["3"] = "extra" }), decide.ReasonLegendKeys, false},
		{"14a score NaN", scoreQ, withScore(func(a *decide.ScoreAnswer) { a.Score = nan }), decide.ReasonNotFinite, false},
		{"14b score range", scoreQ, withScore(func(a *decide.ScoreAnswer) { a.Score = 3 }), decide.ReasonOutOfRange, true},
		{"15 choice sum", choiceQ, withChoice(func(a *decide.ChoiceAnswer) { a.Probabilities = map[string]float64{"a": 0.5, "b": 0.3} }), decide.ReasonProbabilitySum, true},
		{"15 score sum", scoreQ, withScore(func(a *decide.ScoreAnswer) { a.Probabilities["2"] = 0.9 }), decide.ReasonProbabilitySum, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae := validateOne(t, tc.q, tc.a)
			if ae == nil || ae.Reason != tc.reason || ae.Key != "k" {
				t.Fatalf("got %v, want reason %s", ae, tc.reason)
			}
			if !errors.Is(ae, decide.ErrInvalidAnswer) {
				t.Error("does not match ErrInvalidAnswer")
			}
			if got := errors.Is(ae, decide.ErrInconsistentAnswer); got != tc.consistent {
				t.Errorf("ErrInconsistentAnswer = %v, want %v", got, tc.consistent)
			}
		})
	}
}

func TestValidationMissingAndUnexpected(t *testing.T) {
	req := decide.NewRequest("s")
	req.Questions["asked"] = noulQ
	resp, err := stubClient(t, answering(map[string]decide.Answer{"extra": goodChoice()})).SystemOne(t.Context(), req)
	var iae *decide.InvalidAnswersError
	if !errors.As(err, &iae) || len(iae.Answers) != 2 {
		t.Fatalf("err = %v", err)
	}
	// Sorted by key.
	if a := iae.Answers[0]; a.Key != "asked" || a.Reason != decide.ReasonMissingAnswer || a.Type != "noul" {
		t.Errorf("first: %+v", a)
	}
	if a := iae.Answers[1]; a.Key != "extra" || a.Reason != decide.ReasonUnexpectedAnswer || a.Type != "choice" {
		t.Errorf("second: %+v", a)
	}
	if resp.Invalid["asked"] != iae.Answers[0] {
		t.Error("resp.Invalid and InvalidAnswersError disagree")
	}
}

func TestValidationMissingField(t *testing.T) {
	cases := []struct {
		q      decide.Question
		answer string
	}{
		{noulQ, `{"type":"noul"}`},
		{noulQ, `{"type":"noul","noul":null}`},
		{choiceQ, `{"type":"choice","probabilities":{"a":1,"b":0},"confidence":1}`},
		{choiceQ, `{"type":"choice","choice":"a","probabilities":{"a":1,"b":0},"confidence":null}`},
		{scoreQ, `{"type":"score","score":1,"probabilities":{"0":0,"1":1,"2":0},"confidence":1}`},
		{scoreQ, `{"type":"score","legend":{},"probabilities":{"0":0,"1":1,"2":0},"confidence":1}`},
	}
	for _, tc := range cases {
		req := decide.NewRequest("s")
		req.Questions["k"] = tc.q
		resp, err := stubClient(t, decoding(`{"model":"m","answers":{"k":`+tc.answer+`}}`)).SystemOne(t.Context(), req)
		if err == nil || resp.Invalid["k"].Reason != decide.ReasonMissingField {
			t.Errorf("%s: err %v", tc.answer, err)
		}
	}
	// Present zero values are not missing.
	req := decide.NewRequest("s")
	req.Questions["k"] = noulQ
	req.Questions["c"] = choiceQ
	body := `{"model":"m","answers":{"k":{"type":"noul","noul":0},"c":{"type":"choice","choice":"a","probabilities":{"a":0.5,"b":0.5},"confidence":0}}}`
	if _, err := stubClient(t, decoding(body)).SystemOne(t.Context(), req); err != nil {
		t.Fatalf("zero values: %v", err)
	}
}

func TestValidationOrder(t *testing.T) {
	// A *RawAnswer with Err and the wrong type is decode_failed, not type_mismatch.
	if ae := validateOne(t, choiceQ, &decide.RawAnswer{Type: "score", Err: errors.New("bad")}); ae.Reason != decide.ReasonDecodeFailed {
		t.Errorf("raw with Err: %v", ae)
	}
	// The wrong type without Err is type_mismatch.
	if ae := validateOne(t, choiceQ, &decide.RawAnswer{Type: "score"}); ae.Reason != decide.ReasonTypeMismatch {
		t.Errorf("raw without Err: %v", ae)
	}
	// Structural and consistency failures together report only the structural one.
	a := &decide.ChoiceAnswer{Choice: "z", Probabilities: map[string]float64{"a": 0.2, "b": 0.2}, Confidence: 0}
	if ae := validateOne(t, choiceQ, a); ae.Reason != decide.ReasonChoiceNotOption || errors.Is(ae, decide.ErrInconsistentAnswer) {
		t.Errorf("structural first: %v", ae)
	}
}

func TestValidationPasses(t *testing.T) {
	cases := map[string]struct {
		q decide.Question
		a decide.Answer
	}{
		"argmax tie": {choiceQ, &decide.ChoiceAnswer{Choice: "b", Probabilities: map[string]float64{"a": 0.5, "b": 0.5}}},
		// n=2: tol = 0.06.
		"sum at tol": {choiceQ, &decide.ChoiceAnswer{Choice: "a", Probabilities: map[string]float64{"a": 0.53, "b": 0.53}}},
		// n=3: tol = 0.065, so score may reach 2*1.065 = 2.13 or -0.13.
		"score high edge": {scoreQ, func() decide.Answer { a := goodScore(); a.Score = 2.13; return a }()},
		"score low edge":  {scoreQ, func() decide.Answer { a := goodScore(); a.Score = -0.13; return a }()},
		"legend missing keys": {scoreQ, func() decide.Answer {
			a := goodScore()
			delete(a.Legend, "1")
			return a
		}()},
		"legend absent after decode": {scoreQ, goodScoreNoLegend()},
		"raw question":               {&decide.RawQuestion{Type: "rank", JSON: json.RawMessage(`{"type":"rank"}`)}, &decide.RawAnswer{Type: "rank"}},
	}
	for name, tc := range cases {
		if ae := validateOne(t, tc.q, tc.a); ae != nil {
			t.Errorf("%s: %v", name, ae)
		}
	}
	fails := map[string]struct {
		q      decide.Question
		a      decide.Answer
		reason string
	}{
		"sum beyond tol":  {choiceQ, &decide.ChoiceAnswer{Choice: "a", Probabilities: map[string]float64{"a": 0.54, "b": 0.53}}, decide.ReasonProbabilitySum},
		"score beyond hi": {scoreQ, func() decide.Answer { a := goodScore(); a.Score = 2.14; return a }(), decide.ReasonOutOfRange},
		"score beyond lo": {scoreQ, func() decide.Answer { a := goodScore(); a.Score = -0.14; return a }(), decide.ReasonOutOfRange},
	}
	for name, tc := range fails {
		if ae := validateOne(t, tc.q, tc.a); ae == nil || ae.Reason != tc.reason {
			t.Errorf("%s: got %v, want %s", name, ae, tc.reason)
		}
	}
}

// goodScoreNoLegend is a score answer with an empty legend, which check 13
// allows (every legend key present is in range, trivially).
func goodScoreNoLegend() *decide.ScoreAnswer {
	a := goodScore()
	a.Legend = map[string]any{}
	return a
}

func TestValidationOff(t *testing.T) {
	bad := map[string]decide.Answer{"k": decidetest.NoulAnswer(7)}
	req := decide.NewRequest("s")
	req.Questions["k"] = noulQ
	if resp, err := stubClient(t, answering(bad), decide.WithValidation(false)).SystemOne(t.Context(), req); err != nil || resp.Invalid != nil {
		t.Fatalf("WithValidation(false): %v", err)
	}
	c := stubClient(t, answering(bad))
	if _, err := c.SystemOne(t.Context(), req, decide.WithCallValidation(false)); err != nil {
		t.Fatalf("WithCallValidation(false): %v", err)
	}
	if _, err := c.SystemOne(t.Context(), req); err == nil {
		t.Fatal("call option leaked into the next call")
	}
	c = stubClient(t, answering(bad), decide.WithValidation(false))
	if _, err := c.SystemOne(t.Context(), req, decide.WithCallValidation(true)); err == nil {
		t.Fatal("WithCallValidation(true) did not override")
	}
}

// strictQuestion is a custom question with its own validator.
type strictQuestion struct{ err error }

func (strictQuestion) QuestionType() string         { return "tstest-strict" }
func (strictQuestion) MarshalJSON() ([]byte, error) { return []byte(`{"type":"tstest-strict"}`), nil }
func (q strictQuestion) ValidateAnswer(decide.Answer) error {
	return q.err
}

func TestCustomValidator(t *testing.T) {
	sentinel := errors.New("too spicy")
	ae := validateOne(t, strictQuestion{err: sentinel}, &decide.RawAnswer{Type: "tstest-strict"})
	if ae == nil || ae.Reason != decide.ReasonCustom || !errors.Is(ae, sentinel) || ae.Type != "tstest-strict" {
		t.Fatalf("custom: %v", ae)
	}
	mine := &decide.AnswerError{Reason: decide.ReasonOutOfRange, Detail: "mine"}
	ae = validateOne(t, strictQuestion{err: mine}, &decide.RawAnswer{Type: "tstest-strict"})
	if ae == nil || ae.Reason != decide.ReasonOutOfRange || ae.Key != "k" || mine.Key != "" {
		t.Fatalf("AnswerError passthrough: %v (validator's value mutated: %q)", ae, mine.Key)
	}
	if ae := validateOne(t, strictQuestion{}, &decide.RawAnswer{Type: "tstest-strict"}); ae != nil {
		t.Fatalf("nil from validator: %v", ae)
	}
}

func TestInvalidAnswersErrorText(t *testing.T) {
	e := &decide.InvalidAnswersError{Answers: []*decide.AnswerError{
		{Key: "a", Type: "choice", Reason: decide.ReasonChoiceNotOption, Detail: `choice "x" not in options`},
		{Key: "b", Type: "noul", Reason: decide.ReasonMissingAnswer},
		{Key: "c", Type: "noul", Reason: decide.ReasonMissingAnswer},
	}}
	want := `decide: 3 invalid answers: answer "a" (choice): choice_not_option: choice "x" not in options; answer "b" (noul): missing_answer; ...`
	if e.Error() != want {
		t.Fatalf("got  %s\nwant %s", e.Error(), want)
	}
	if !errors.Is(e, decide.ErrInvalidAnswer) || errors.Is(e, decide.ErrInconsistentAnswer) {
		t.Fatal("aggregate Is")
	}
}
