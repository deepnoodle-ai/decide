package heads

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/deepnoodle-ai/decide"
)

// Diagnostic reports an invalid answer. Branch and Local name a question
// in this plan; both are empty for a pre-existing question or an unexpected
// response key. The selector uses Local "$selector" and an empty Branch.
type Diagnostic struct {
	Branch string
	Local  string
	Key    string
	Err    error
}

// ReadError reports a bad selector, selected answer, or typed access.
// It wraps ErrInvalidAnswer and the underlying validation error.
type ReadError struct {
	Branch string
	Local  string
	Key    string
	Err    error
}

func (e *ReadError) Error() string {
	key := ""
	if e.Key != "" {
		key = " (" + e.Key + ")"
	}
	if e.Branch == "" {
		return fmt.Sprintf("heads: %s%s: %v", e.Local, key, e.Err)
	}
	return fmt.Sprintf("heads: branch %q question %q%s: %v", e.Branch, e.Local, key, e.Err)
}

func (e *ReadError) Unwrap() []error {
	return []error{decide.ErrInvalidAnswer, e.Err}
}

// Result contains only the selected branch's answers. Diagnostics contain
// validation failures for all plan questions, pre-existing questions, and
// unexpected response keys. The original Response remains with the caller.
type Result struct {
	Selector    *decide.ChoiceAnswer
	Branch      string
	Answers     map[string]decide.Answer
	Diagnostics []Diagnostic
}

// Read validates the selector and every selected answer, even when the root
// client did not validate the response. It reports unused-branch failures
// without letting them block a valid selected branch. A response returned
// alongside a root InvalidAnswersError may be passed here after the caller
// decides the API call itself is safe to continue from.
func (b *Binding) Read(resp *decide.Response) (Result, error) {
	result := Result{}
	if b == nil || b.request == nil {
		return result, &ReadError{Local: "$selector", Err: fmt.Errorf("nil binding")}
	}
	if resp == nil {
		return result, &ReadError{Local: "$selector", Key: b.selectorKey, Err: fmt.Errorf("nil response")}
	}
	bad := make(map[string]error)
	keys := make(map[string]bool, len(b.owned)+len(b.request.Questions)+len(resp.Answers)+len(resp.Invalid))
	for key := range b.owned {
		keys[key] = true
	}
	for key := range b.request.Questions {
		keys[key] = true
	}
	for key := range resp.Answers {
		keys[key] = true
	}
	for key := range resp.Invalid {
		keys[key] = true
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		var q decide.Question
		var d Diagnostic
		d.Key = key
		if own, ok := b.owned[key]; ok {
			q = own.q
			d.Branch, d.Local = own.branch, own.local
		} else if external, ok := b.request.Questions[key]; ok {
			q = external
		}
		var answer decide.Answer
		if resp.Answers != nil {
			answer = resp.Answers[key]
		}
		err := validate(key, q, answer)
		if err == nil && resp.Invalid != nil {
			if invalid := resp.Invalid[key]; invalid != nil {
				err = invalid
			}
		}
		if err == nil {
			continue
		}
		d.Err = err
		result.Diagnostics = append(result.Diagnostics, d)
		bad[key] = err
	}
	if err := bad[b.selectorKey]; err != nil {
		return result, &ReadError{Local: "$selector", Key: b.selectorKey, Err: err}
	}
	selector, ok := resp.Answers[b.selectorKey].(*decide.ChoiceAnswer)
	if !ok || selector == nil {
		return result, &ReadError{Local: "$selector", Key: b.selectorKey,
			Err: fmt.Errorf("answer is not a Choice")}
	}
	result.Selector = selector
	locals, ok := b.byBranch[selector.Choice]
	if !ok {
		return result, &ReadError{Local: "$selector", Key: b.selectorKey,
			Err: fmt.Errorf("unknown selected option %q", selector.Choice)}
	}
	result.Branch = selector.Choice
	for _, local := range slices.Sorted(maps.Keys(locals)) {
		key := locals[local]
		if err := bad[key]; err != nil {
			return result, &ReadError{Branch: result.Branch, Local: local, Key: key, Err: err}
		}
	}
	result.Answers = make(map[string]decide.Answer, len(locals))
	for local, key := range locals {
		result.Answers[local] = resp.Answers[key]
	}
	return result, nil
}

// AnswerAs reads one selected-branch answer as A. A missing local name or
// wrong Go answer type returns *ReadError; no unchecked assertion is needed.
func AnswerAs[A decide.Answer](r Result, local string) (A, error) {
	var zero A
	a, ok := r.Answers[local]
	if !ok || nilValue(a) {
		return zero, &ReadError{Branch: r.Branch, Local: local,
			Err: fmt.Errorf("no selected answer")}
	}
	typed, ok := a.(A)
	if !ok || nilValue(typed) {
		return zero, &ReadError{Branch: r.Branch, Local: local,
			Err: fmt.Errorf("answer type %T does not match requested type", a)}
	}
	return typed, nil
}

func validate(key string, q decide.Question, a decide.Answer) error {
	if q == nil {
		typ := ""
		if !nilValue(a) {
			typ = a.AnswerType()
		}
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonUnexpectedAnswer,
			Detail: "no question with this key"}
	}
	if nilValue(q) {
		return &decide.AnswerError{Key: key,
			Reason: decide.ReasonCustom,
			Detail: "question is a typed nil"}
	}
	typ := q.QuestionType()
	if nilValue(a) {
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonMissingAnswer,
			Detail: "no answer for question"}
	}
	if raw, ok := a.(*decide.RawAnswer); ok && raw.Err != nil {
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonDecodeFailed, Err: raw.Err}
	}
	if got := a.AnswerType(); got != typ {
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonTypeMismatch,
			Detail: fmt.Sprintf("answer type %q, question type %q", got, typ)}
	}
	validator, ok := q.(decide.AnswerValidator)
	if !ok {
		return nil
	}
	if err := validator.ValidateAnswer(a); err != nil {
		var answerErr *decide.AnswerError
		if errors.As(err, &answerErr) {
			copyErr := *answerErr
			copyErr.Key = key
			if copyErr.Type == "" {
				copyErr.Type = typ
			}
			return &copyErr
		}
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonCustom, Err: err}
	}
	return nil
}
