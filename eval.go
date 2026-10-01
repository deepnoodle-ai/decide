package decide

import (
	"context"
	"errors"
	"reflect"
	"strconv"
)

// Evaluation contains a typed judgment and the response that supplied it.
// Response retains model, usage, request ID, headers, and validation failures.
type Evaluation[A Answer] struct {
	Answer   A
	Response *Response
}

// Eval asks one typed question under the key "eval". It always validates
// the answer, even when client-wide validation is disabled. External
// QuestionFor implementations need no answer registration.
//
// Structural failures return no typed Answer. Consistency-only failures
// return the full Answer and an error matching ErrInconsistentAnswer.
// Every response returned by Client.SystemOne remains available in Response.
// Use NewRequest, Ask, and Client.SystemOne for multiple questions.
func Eval[A Answer](ctx context.Context, client *Client, state any, question QuestionFor[A], opts ...RequestOption) (Evaluation[A], error) {
	return evalOne(ctx, client, state, "eval", question, opts...)
}

// Candidate pairs an original item with the JSON description sent to the
// model. Item is returned unchanged; only Description enters the question.
type Candidate[T any] struct {
	Item        T
	Description any
}

// Decision contains an original candidate or an explicit abstention.
// Index is -1 and Item is zero when Picked is false. Answer preserves the
// original Choice evidence; Response preserves the complete client response.
// Inspect the error before using a selection: consistency-only errors retain
// a selected item, while structural failures do not.
type Decision[T any] struct {
	Item     T
	Index    int
	Picked   bool
	Answer   *ChoiceAnswer
	Response *Response
}

// Pick asks the model to choose one described candidate or abstain. It sends
// positional keys c1, c2, ... and the abstain key "none" under question "pick".
// At most 254 candidates are accepted. Empty candidates abstain without a
// network call, Answer, or Response. A canceled context still returns an error.
//
// Pick always validates the complete response. Errors never become successful
// abstentions. No thresholds or application actions are applied.
func Pick[T any](ctx context.Context, client *Client, state any, instructions any, candidates []Candidate[T], opts ...RequestOption) (Decision[T], error) {
	d := Decision[T]{Index: -1}
	if err := checkEvalCall(ctx, client); err != nil {
		return d, err
	}
	if len(candidates) == 0 {
		return d, nil
	}
	if len(candidates) > 254 {
		return d, invalidRequest("Pick accepts at most 254 candidates")
	}
	options := make([]ChoiceOption, 0, len(candidates)+1)
	for i, candidate := range candidates {
		options = append(options, ChoiceOption{Key: "c" + strconv.Itoa(i+1), Description: candidate.Description})
	}
	options = append(options, ChoiceOption{Key: "none", Description: "None of these is the requested value."})
	e, err := evalOne(ctx, client, state, "pick", Choice(instructions, options...), opts...)
	d.Answer, d.Response = e.Answer, e.Response
	if e.Answer == nil || e.Answer.Choice == "none" {
		return d, err
	}
	// Complete answer validation guarantees this is one of our positional keys.
	i, parseErr := strconv.Atoi(e.Answer.Choice[1:])
	if parseErr != nil || i < 1 || i > len(candidates) {
		return d, &AnswerError{Key: "pick", Type: typeChoice, Reason: ReasonChoiceNotOption}
	}
	d.Item, d.Index, d.Picked = candidates[i-1].Item, i-1, true
	return d, err
}

func checkEvalCall(ctx context.Context, client *Client) error {
	if ctx == nil {
		return invalidRequest("context is nil")
	}
	if client == nil {
		return invalidRequest("client is nil")
	}
	return ctx.Err()
}

func evalOne[A Answer](ctx context.Context, client *Client, state any, key string, question QuestionFor[A], opts ...RequestOption) (Evaluation[A], error) {
	var e Evaluation[A]
	if err := checkEvalCall(ctx, client); err != nil {
		return e, err
	}
	if nilQuestionFor(question) {
		return e, invalidRequest("question is nil")
	}
	req := NewRequest(state, opts...)
	req.Questions = map[string]Question{key: question}
	// Validate the original question before wrapping it so built-in request
	// checks (choice options, score levels, byte values) still apply.
	if err := req.Validate(); err != nil {
		return e, err
	}
	req.Questions[key] = evalQuestion[A]{question}
	resp, err := client.SystemOne(ctx, req, WithCallValidation(true))
	e.Response = resp
	if err != nil && !onlyConsistencyErrors(err) {
		return e, err
	}
	h := Handle[A]{key: key, qtype: question.QuestionType()}
	a, readErr := h.From(resp)
	if readErr != nil && !onlyConsistencyErrors(readErr) {
		return e, readErr
	}
	e.Answer = a
	if err != nil {
		return e, err
	}
	return e, readErr
}

// The adapter supplies the inferred constructor without global registration.
// Optional custom validation still belongs to the caller's question.
type evalQuestion[A Answer] struct{ QuestionFor[A] }

func (q evalQuestion[A]) MakeAnswer() Answer { return q.NewAnswer() }

func (q evalQuestion[A]) ValidateAnswer(a Answer) error {
	if validator, ok := q.QuestionFor.(AnswerValidator); ok {
		return validator.ValidateAnswer(a)
	}
	return nil
}

func nilQuestionFor(q any) bool {
	v := reflect.ValueOf(q)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func onlyConsistencyErrors(err error) bool {
	if ae, ok := err.(*AnswerError); ok {
		return ae != nil && ae.consistency() && ae.Err == nil
	}
	if all, ok := errors.AsType[*InvalidAnswersError](err); ok && len(all.Answers) > 0 {
		for _, ae := range all.Answers {
			if ae == nil || !ae.consistency() || ae.Err != nil {
				return false
			}
		}
		return true
	}
	return false
}
