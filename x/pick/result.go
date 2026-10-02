package pick

import (
	"errors"
	"fmt"
	"iter"

	"github.com/deepnoodle-ai/decide"
)

// Result is one pick read back from a choice answer.
type Result[T any] struct {
	Item     T                    // the candidate itself; zero value when !Picked
	Index    int                  // index into the items given to New; -1 when !Picked
	Picked   bool                 // false when the abstain option was chosen
	Key      string               // the chosen option key (the abstain key when !Picked)
	P        float64              // Probabilities[Key], as received
	AbstainP float64              // Probabilities[abstain key], as received
	RunnerUp string               // highest-probability key other than Key, abstain included; "" if none
	Margin   float64              // P minus Probabilities[RunnerUp]; P when RunnerUp is ""
	Answer   *decide.ChoiceAnswer // unchanged; Confidence passes through here
}

// Read maps a choice answer back to an item.
//
// A choice equal to the abstain key gives Picked == false and Index -1. A
// choice that is neither a candidate key nor the abstain key returns an
// *UnmappedChoiceError, never a silent abstain. A nil answer returns an
// error wrapping decide.ErrInvalidAnswer.
//
// Read assumes an answer the client validated (the default): a key absent
// from Probabilities reads as 0, and Read does not repeat the client's
// probability-key or argmax checks. RunnerUp and Margin are measured from
// the chosen key, so on an argmax tie within the client's tolerance Margin
// can be slightly negative, and on an unvalidated argmax mismatch it is
// plainly negative. It is reported as computed, not clamped.
func (p *Picker[T]) Read(a *decide.ChoiceAnswer) (Result[T], error) {
	if a == nil {
		return Result[T]{Index: -1}, fmt.Errorf("pick: nil answer: %w", decide.ErrInvalidAnswer)
	}
	if _, ok := p.index[a.Choice]; !ok && a.Choice != p.abstainKey {
		return Result[T]{Index: -1, Answer: a}, &UnmappedChoiceError{Choice: a.Choice}
	}
	return p.result(a, a.Choice, a.Ranked()), nil
}

// Ranked yields (result, probability) for every key in a.Probabilities,
// most probable first, ties by key ascending: the order a.Ranked() uses.
// Each Result is built as Read would build it had that key been chosen.
// The abstain option, and any key that maps to no candidate, yield a
// Result with Picked == false. A nil answer yields nothing.
func (p *Picker[T]) Ranked(a *decide.ChoiceAnswer) iter.Seq2[Result[T], float64] {
	return func(yield func(Result[T], float64) bool) {
		if a == nil {
			return
		}
		ranked := a.Ranked()
		for _, e := range ranked {
			if !yield(p.result(a, e.Key, ranked), e.P) {
				return
			}
		}
	}
}

// result builds the Result for key. ranked is a.Ranked(); the runner-up is
// its first entry other than key, which is position 0 or 1.
func (p *Picker[T]) result(a *decide.ChoiceAnswer, key string, ranked []decide.Prob) Result[T] {
	r := Result[T]{
		Index:    -1,
		Key:      key,
		P:        a.Probabilities[key],
		AbstainP: a.Probabilities[p.abstainKey],
		Answer:   a,
	}
	if i, ok := p.index[key]; ok {
		r.Item, r.Index, r.Picked = p.items[i], i, true
	}
	r.Margin = r.P
	for _, e := range ranked {
		if e.Key != key {
			r.RunnerUp, r.Margin = e.Key, r.P-e.P
			break
		}
	}
	return r
}

// Handle is a typed reference to one pick in a request.
type Handle[T any] struct {
	h decide.Handle[*decide.ChoiceAnswer]
	p *Picker[T]
}

// Key returns the question key.
func (h Handle[T]) Key() string { return h.h.Key() }

// From reads the answer for h's key and calls Read.
//
// A nil response, a missing answer, a wrong answer type, or a structural
// validation failure in resp.Invalid (for example choice_not_option) is
// returned unchanged with a zero Result. A consistency-only failure, one
// matching decide.ErrInconsistentAnswer (the choice is not the argmax, or
// the probabilities do not sum to about 1), returns the fully populated
// Result together with that same error, as decide.Handle.From does: the
// probabilities are reported as received, Key is still the server's choice,
// and on an argmax mismatch Margin is negative. To read on, write
//
//	if err != nil && !errors.Is(err, decide.ErrInconsistentAnswer) {
//		return err
//	}
func (h Handle[T]) From(resp *decide.Response) (Result[T], error) {
	// The client handle returns the answer together with resp.Invalid[key]
	// when the stored answer has the right type, so the consistency case
	// needs no second lookup here.
	a, err := h.h.From(resp)
	if err != nil && (a == nil || !errors.Is(err, decide.ErrInconsistentAnswer)) {
		return Result[T]{Index: -1}, err
	}
	r, rerr := h.p.Read(a)
	if rerr != nil {
		return r, rerr
	}
	return r, err // err is nil or the client's consistency error
}
