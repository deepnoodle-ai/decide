package heads

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func routeAnswer(choice string) *decide.ChoiceAnswer {
	probs := map[string]float64{"bug": 0.1, "billing": 0.1}
	probs[choice] = 0.9
	return &decide.ChoiceAnswer{Choice: choice, Probabilities: probs, Confidence: 0.8}
}

func setupRead(t *testing.T) (*decide.Request, *Binding, string, string) {
	t.Helper()
	req := decide.NewRequest("ticket")
	req.Questions["common"] = decide.Noul("Is this urgent?")
	b, err := testPlan(t).Attach(req, "route")
	if err != nil {
		t.Fatal(err)
	}
	severity, _ := b.Key("bug", "severity")
	refund, _ := b.Key("billing", "refund")
	return req, b, severity, refund
}

func validResponse(severity, refund string) *decide.Response {
	return &decide.Response{Answers: map[string]decide.Answer{
		"route":  routeAnswer("billing"),
		"common": decidetest.NoulAnswer(0.7),
		severity: decidetest.ScoreAnswer([]any{"low", "high"}, 0.2, 0.8),
		refund:   decidetest.NoulAnswer(0.9),
	}}
}

func TestReadSelectedAndTyped(t *testing.T) {
	_, b, severity, refund := setupRead(t)
	r, err := b.Read(validResponse(severity, refund))
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "billing" || len(r.Answers) != 1 || r.Answers["refund"] == nil {
		t.Fatalf("wrong selected result: %+v", r)
	}
	if _, ok := r.Answers["severity"]; ok {
		t.Fatal("unselected answer leaked")
	}
	a, err := AnswerAs[*decide.NoulAnswer](r, "refund")
	if err != nil || a.Noul != 0.9 {
		t.Fatalf("typed answer: %v, %v", a, err)
	}
	if _, err := AnswerAs[*decide.ScoreAnswer](r, "refund"); err == nil {
		t.Fatal("wrong typed access accepted")
	}
	if _, err := AnswerAs[*decide.NoulAnswer](r, "severity"); err == nil {
		t.Fatal("unselected typed access accepted")
	}
	resp := validResponse(severity, refund)
	resp.Invalid = map[string]*decide.AnswerError{
		refund: {Key: refund, Type: "noul", Reason: decide.ReasonCustom,
			Detail: "root rejected answer"},
	}
	_, err = b.Read(resp)
	var readErr *ReadError
	if !errors.As(err, &readErr) || readErr.Local != "refund" {
		t.Fatalf("selected root diagnostic: %v", err)
	}
}

func TestReadEachPopulatedRoute(t *testing.T) {
	_, b, severity, refund := setupRead(t)
	for _, tc := range []struct {
		branch string
		local  string
		other  string
	}{
		{branch: "bug", local: "severity", other: "refund"},
		{branch: "billing", local: "refund", other: "severity"},
	} {
		t.Run(tc.branch, func(t *testing.T) {
			resp := validResponse(severity, refund)
			resp.Answers["route"] = routeAnswer(tc.branch)
			r, err := b.Read(resp)
			if err != nil {
				t.Fatal(err)
			}
			if r.Branch != tc.branch || len(r.Answers) != 1 ||
				r.Answers[tc.local] == nil || len(r.Diagnostics) != 0 {
				t.Fatalf("wrong selected answers: %+v", r)
			}
			if _, ok := r.Answers[tc.other]; ok {
				t.Fatalf("unselected %q answer leaked into %q", tc.other, tc.branch)
			}
		})
	}
}

func TestReadEmptyBranch(t *testing.T) {
	p, err := New(
		decide.Choice("Which?", decide.Option("empty"), decide.Option("full")),
		Branch{Option: "empty"},
		Branch{Option: "full", Questions: map[string]decide.Question{
			"check": decide.Noul("Does this apply?"),
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Attach(decide.NewRequest("state"), "route")
	if err != nil {
		t.Fatal(err)
	}
	fullKey, _ := b.Key("full", "check")
	for _, tc := range []struct {
		branch string
		local  string
		count  int
	}{
		{branch: "empty", count: 0},
		{branch: "full", local: "check", count: 1},
	} {
		t.Run(tc.branch, func(t *testing.T) {
			probs := map[string]float64{"empty": 0.1, "full": 0.1}
			probs[tc.branch] = 0.9
			resp := &decide.Response{Answers: map[string]decide.Answer{
				"route": &decide.ChoiceAnswer{
					Choice: tc.branch, Probabilities: probs, Confidence: 0.8,
				},
				fullKey: decidetest.NoulAnswer(0.7),
			}}
			r, err := b.Read(resp)
			if err != nil || r.Branch != tc.branch || len(r.Answers) != tc.count ||
				len(r.Diagnostics) != 0 {
				t.Fatalf("selected branch: %+v, %v", r, err)
			}
			if tc.local != "" && r.Answers[tc.local] == nil {
				t.Fatalf("missing selected answer %q: %+v", tc.local, r)
			}
			if tc.local == "" {
				if _, ok := r.Answers["check"]; ok {
					t.Fatal("full branch answer leaked into empty branch")
				}
			}
		})
	}
}

func TestReadValidatesDirectAndDisabledRoot(t *testing.T) {
	req, b, severity, refund := setupRead(t)
	resp := validResponse(severity, refund)
	resp.Answers[refund] = decidetest.NoulAnswer(2)
	_, err := b.Read(resp)
	var readErr *ReadError
	if !errors.As(err, &readErr) || readErr.Branch != "billing" || readErr.Local != "refund" ||
		!errors.Is(err, decide.ErrInvalidAnswer) {
		t.Fatalf("direct response error: %v", err)
	}
	srv := decidetest.NewServer(t)
	srv.Answer("route", routeAnswer("billing"))
	srv.Answer(refund, decidetest.NoulAnswer(2))
	client := srv.Client(t, decide.WithValidation(false))
	resp, err = client.SystemOne(context.Background(), req)
	if err != nil || resp.Invalid != nil {
		t.Fatalf("root validation was not disabled: %v", err)
	}
	_, err = b.Read(resp)
	if !errors.As(err, &readErr) || readErr.Local != "refund" {
		t.Fatalf("disabled-root validation error: %v", err)
	}
}

func TestReadUnusedAndExternalDiagnostics(t *testing.T) {
	req, b, severity, refund := setupRead(t)
	req.Questions["late_common"] = decide.Noul("A common question added later")
	resp := validResponse(severity, refund)
	resp.Answers[severity] = decidetest.NoulAnswer(0.5)
	resp.Answers["common"] = decidetest.NoulAnswer(-1)
	resp.Answers["late_common"] = decidetest.NoulAnswer(-1)
	resp.Answers["surprise"] = decidetest.NoulAnswer(0.5)
	r, err := b.Read(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 4 {
		t.Fatalf("diagnostics: %+v", r.Diagnostics)
	}
	byKey := map[string]Diagnostic{}
	for _, d := range r.Diagnostics {
		byKey[d.Key] = d
	}
	if d := byKey[severity]; d.Branch != "bug" || d.Local != "severity" {
		t.Fatalf("unused branch diagnostic: %+v", d)
	}
	for _, key := range []string{"common", "late_common", "surprise"} {
		if d := byKey[key]; d.Branch != "" || d.Local != "" || d.Err == nil {
			t.Fatalf("external diagnostic %s: %+v", key, d)
		}
	}
}

type nilCommonQuestion struct{ kind string }

func (q *nilCommonQuestion) QuestionType() string { return q.kind }
func (q *nilCommonQuestion) MarshalJSON() ([]byte, error) {
	return []byte(`{"type":"noul","instructions":"common"}`), nil
}

func TestReadTypedNilCommonQuestion(t *testing.T) {
	req, b, severity, refund := setupRead(t)
	var nilQuestion *nilCommonQuestion
	req.Questions["late_common"] = nilQuestion
	resp := validResponse(severity, refund)
	resp.Answers["late_common"] = decidetest.NoulAnswer(0.5)
	r, err := b.Read(resp)
	if err != nil || r.Branch != "billing" || len(r.Answers) != 1 {
		t.Fatalf("selected branch changed: %+v, %v", r, err)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Key != "late_common" ||
		r.Diagnostics[0].Branch != "" || r.Diagnostics[0].Local != "" {
		t.Fatalf("typed-nil common diagnostic: %+v", r.Diagnostics)
	}
	var answerErr *decide.AnswerError
	if !errors.As(r.Diagnostics[0].Err, &answerErr) ||
		answerErr.Reason != decide.ReasonCustom ||
		answerErr.Key != "late_common" ||
		!errors.Is(r.Diagnostics[0].Err, decide.ErrInvalidAnswer) {
		t.Fatalf("typed-nil common error: %v", r.Diagnostics[0].Err)
	}
}

func TestReadInvalidSelectorAndMissingSelected(t *testing.T) {
	_, b, severity, refund := setupRead(t)
	cases := []struct {
		name   string
		mutate func(*decide.Response)
		local  string
	}{
		{"missing selector", func(r *decide.Response) { delete(r.Answers, "route") }, "$selector"},
		{"unknown choice", func(r *decide.Response) { r.Answers["route"] = routeAnswer("other") }, "$selector"},
		{"missing selected", func(r *decide.Response) { delete(r.Answers, refund) }, "refund"},
		{"wrong selected type", func(r *decide.Response) { r.Answers[refund] = decidetest.ScoreAnswer([]any{"x", "y"}, 0.5, 0.5) }, "refund"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := validResponse(severity, refund)
			tc.mutate(resp)
			_, err := b.Read(resp)
			var readErr *ReadError
			if !errors.As(err, &readErr) || readErr.Local != tc.local {
				t.Fatalf("got %v, want local %s", err, tc.local)
			}
		})
	}
}

func TestFakeServerOneRequestAndUnusedFailure(t *testing.T) {
	req, b, severity, refund := setupRead(t)
	srv := decidetest.NewServer(t)
	srv.Answer("route", routeAnswer("billing"))
	srv.Answer(severity, decidetest.NoulAnswer(0.5))
	srv.Answer(refund, decidetest.NoulAnswer(0.8))
	resp, callErr := srv.Client(t).SystemOne(context.Background(), req)
	if !errors.Is(callErr, decide.ErrInvalidAnswer) || resp == nil {
		t.Fatalf("expected response and invalid unused answer, got %v", callErr)
	}
	r, err := b.Read(resp)
	if err != nil || r.Branch != "billing" || len(r.Diagnostics) != 1 {
		t.Fatalf("selected read: %+v, %v", r, err)
	}
	requests := srv.Requests()
	if len(requests) != 1 || len(requests[0].Request.Questions) != 4 {
		t.Fatalf("requests: %+v", requests)
	}
	if !strings.Contains(r.Diagnostics[0].Err.Error(), severity) ||
		r.Diagnostics[0].Local != "severity" {
		t.Fatalf("unused diagnostic: %+v", r.Diagnostics[0])
	}
}

type customQuestion struct{}

func (*customQuestion) QuestionType() string { return "custom" }
func (*customQuestion) MarshalJSON() ([]byte, error) {
	return []byte(`{"type":"custom","instructions":"test"}`), nil
}
func (*customQuestion) ValidateAnswer(decide.Answer) error {
	return errors.New("custom validation failed")
}

func TestReadCustomValidatorAndRootDiagnostic(t *testing.T) {
	p, err := New(
		decide.Choice("route", decide.Option("custom"), decide.Option("empty")),
		Branch{Option: "custom", Questions: map[string]decide.Question{
			"value": &customQuestion{},
		}},
		Branch{Option: "empty"},
	)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Attach(decide.NewRequest("state"), "route")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := b.Key("custom", "value")
	resp := &decide.Response{Answers: map[string]decide.Answer{
		"route": &decide.ChoiceAnswer{Choice: "custom", Probabilities: map[string]float64{
			"custom": 0.9, "empty": 0.1,
		}, Confidence: 0.8},
		key: &decide.RawAnswer{Type: "custom", JSON: []byte(`{"type":"custom"}`)},
	}}
	_, err = b.Read(resp)
	var readErr *ReadError
	if !errors.As(err, &readErr) || readErr.Local != "value" ||
		!strings.Contains(err.Error(), "custom validation failed") {
		t.Fatalf("custom validator: %v", err)
	}
	resp.Answers["route"] = &decide.ChoiceAnswer{Choice: "empty", Probabilities: map[string]float64{
		"custom": 0.1, "empty": 0.9,
	}, Confidence: 0.8}
	resp.Invalid = map[string]*decide.AnswerError{
		key: {Key: key, Type: "custom", Reason: decide.ReasonCustom,
			Detail: "root diagnostic"},
	}
	r, err := b.Read(resp)
	if err != nil || r.Branch != "empty" || len(r.Diagnostics) != 1 ||
		r.Diagnostics[0].Local != "value" {
		t.Fatalf("unused custom diagnostic: %+v, %v", r, err)
	}
	resp.Answers["route"] = &decide.ChoiceAnswer{Choice: "custom", Probabilities: map[string]float64{
		"custom": 0.9, "empty": 0.1,
	}, Confidence: 0.8}
	_, err = b.Read(resp)
	if !errors.As(err, &readErr) || readErr.Branch != "custom" ||
		readErr.Local != "value" {
		t.Fatalf("selected root diagnostic: %v", err)
	}
}
