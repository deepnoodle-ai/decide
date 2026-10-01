package sod

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
)

// eps tolerates serialization noise at bounds.
const eps = 1e-6

// tolerance is the provisional bound for checks 14b and 15, where n is the
// number of options (Choice) or levels (Score).
//
// PROVISIONAL: the docs and spec say probabilities sum to "approximately 1"
// and do not document rounding. The live validation corpus (live_test.go)
// must show zero failures before this is tightened.
func tolerance(n int) float64 { return 0.05 + 0.005*float64(n) }

// validateResponse checks every answer in resp against req. Checks stop at
// the first failure per key, so each key has at most one *AnswerError. It
// returns nil, nil when every answer is valid.
func validateResponse(req *Request, resp *Response) (map[string]*AnswerError, error) {
	invalid := make(map[string]*AnswerError)
	for key, q := range req.Questions {
		if ae := validateOne(key, q, resp.Answers[key]); ae != nil {
			invalid[key] = ae
		}
	}
	for key, a := range resp.Answers {
		if _, ok := req.Questions[key]; ok {
			continue
		}
		typ := ""
		if a != nil {
			typ = a.AnswerType()
		}
		invalid[key] = &AnswerError{Key: key, Type: typ, Reason: ReasonUnexpectedAnswer, Detail: "no question with this key"} // check 2
	}
	if len(invalid) == 0 {
		return nil, nil
	}
	e := &InvalidAnswersError{}
	for _, k := range slices.Sorted(maps.Keys(invalid)) {
		e.Answers = append(e.Answers, invalid[k])
	}
	return invalid, e
}

// validateOne runs checks 1, the RawAnswer.Err rule, 4, then the
// question's ValidateAnswer (for the built-ins: 3, 0, 5 to 15).
func validateOne(key string, q Question, a Answer) *AnswerError {
	qt := q.QuestionType()
	if a == nil {
		return &AnswerError{Key: key, Type: qt, Reason: ReasonMissingAnswer, Detail: "no answer for question"} // check 1
	}
	if raw, ok := a.(*RawAnswer); ok && raw.Err != nil {
		return &AnswerError{Key: key, Type: qt, Reason: ReasonDecodeFailed, Err: raw.Err}
	}
	if at := a.AnswerType(); at != qt {
		return &AnswerError{Key: key, Type: qt, Reason: ReasonTypeMismatch,
			Detail: fmt.Sprintf("answer type %q, question type %q", at, qt)} // check 4
	}
	v, ok := q.(AnswerValidator)
	if !ok {
		return nil
	}
	err := v.ValidateAnswer(a)
	if err == nil {
		return nil
	}
	ae, ok := errors.AsType[*AnswerError](err)
	if !ok {
		ae = &AnswerError{Reason: ReasonCustom, Err: err}
	} else {
		cp := *ae // do not mutate the validator's value
		ae = &cp
	}
	ae.Key = key
	ae.Type = cmp.Or(ae.Type, qt)
	return ae
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func fail(reason, format string, args ...any) *AnswerError {
	return &AnswerError{Reason: reason, Detail: fmt.Sprintf(format, args...)}
}

func missingField(missing []string) *AnswerError {
	if len(missing) == 0 {
		return nil
	}
	return fail(ReasonMissingField, "missing or null: %v", missing) // check 0
}

// checkUnit is checks 5/6 and 7/8 for one value: finite, then in [0,1].
func checkUnit(name string, f float64) *AnswerError {
	if !finite(f) {
		return fail(ReasonNotFinite, "%s is %v", name, f)
	}
	if f < -eps || f > 1+eps {
		return fail(ReasonOutOfRange, "%s is %v, not in [0,1]", name, f)
	}
	return nil
}

// checkDistribution is checks 7 then 8 over probabilities and confidence.
func checkDistribution(probs map[string]float64, confidence float64) *AnswerError {
	names := append([]string{"confidence"}, slices.Sorted(maps.Keys(probs))...)
	value := func(n string, i int) float64 {
		if i == 0 {
			return confidence
		}
		return probs[n]
	}
	for i, n := range names { // all of check 7 before any of check 8
		if f := value(n, i); !finite(f) {
			return fail(ReasonNotFinite, "%s is %v", label(n, i), f)
		}
	}
	for i, n := range names {
		if ae := checkUnit(label(n, i), value(n, i)); ae != nil {
			return ae
		}
	}
	return nil
}

func label(n string, i int) string {
	if i == 0 {
		return n
	}
	return fmt.Sprintf("probability %q", n)
}

// checkSum is check 15.
func checkSum(probs map[string]float64, n int) *AnswerError {
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	// eps absorbs float summation error at the bound.
	if tol := tolerance(n); math.Abs(sum-1) > tol+eps {
		return fail(ReasonProbabilitySum, "probabilities sum to %v, tolerance %v", sum, tol)
	}
	return nil
}

// ValidateAnswer checks a noul answer: it is a *NoulAnswer with "noul"
// present, finite, and in [0,1].
func (q *NoulQuestion) ValidateAnswer(a Answer) error {
	na, ok := a.(*NoulAnswer)
	if !ok || na == nil {
		return fail(ReasonDecodeFailed, "answer is %T, want *NoulAnswer", a) // check 3
	}
	if ae := missingField(na.missing); ae != nil {
		return ae
	}
	if ae := checkUnit("noul", na.Noul); ae != nil { // checks 5, 6
		return ae
	}
	return nil
}

// ValidateAnswer checks a choice answer against the options. Structural:
// fields present, values finite and in [0,1], probability keys equal the
// option keys, the choice is an option. Consistency: the choice is the
// argmax (ties allowed) and probabilities sum to about 1.
func (q *ChoiceQuestion) ValidateAnswer(a Answer) error {
	if q == nil {
		return fail(ReasonCustom, "nil *ChoiceQuestion")
	}
	ca, ok := a.(*ChoiceAnswer)
	if !ok || ca == nil {
		return fail(ReasonDecodeFailed, "answer is %T, want *ChoiceAnswer", a) // check 3
	}
	if ae := missingField(ca.missing); ae != nil {
		return ae
	}
	if ae := checkDistribution(ca.Probabilities, ca.Confidence); ae != nil { // checks 7, 8
		return ae
	}
	options := make(map[string]bool, len(q.Criteria))
	for _, o := range q.Criteria {
		options[o.Key] = true
	}
	if !sameKeys(ca.Probabilities, options) { // check 9
		return fail(ReasonProbabilityKeys, "probability keys %v, option keys %v",
			slices.Sorted(maps.Keys(ca.Probabilities)), slices.Sorted(maps.Keys(options)))
	}
	if !options[ca.Choice] { // check 10
		return fail(ReasonChoiceNotOption, "choice %q not in options", ca.Choice)
	}
	top := math.Inf(-1)
	for _, p := range ca.Probabilities {
		top = max(top, p)
	}
	if p := ca.Probabilities[ca.Choice]; p < top-eps { // check 11
		return &AnswerError{Reason: ReasonChoiceNotArgmax, Detail: fmt.Sprintf("choice %q has P=%v, max is %v", ca.Choice, p, top)}
	}
	if ae := checkSum(ca.Probabilities, len(q.Criteria)); ae != nil { // check 15
		return ae
	}
	return nil
}

// ValidateAnswer checks a score answer against the levels. Structural:
// fields present, values finite and in [0,1], probability keys are exactly
// "0".."n-1", every legend key is one of them (missing legend keys are
// allowed), score finite. Consistency: score within tolerance of [0, n-1]
// and probabilities sum to about 1.
func (q *ScoreQuestion) ValidateAnswer(a Answer) error {
	if q == nil {
		return fail(ReasonCustom, "nil *ScoreQuestion")
	}
	sa, ok := a.(*ScoreAnswer)
	if !ok || sa == nil {
		return fail(ReasonDecodeFailed, "answer is %T, want *ScoreAnswer", a) // check 3
	}
	if ae := missingField(sa.missing); ae != nil {
		return ae
	}
	if ae := checkDistribution(sa.Probabilities, sa.Confidence); ae != nil { // checks 7, 8
		return ae
	}
	n := len(q.Criteria)
	levels := make(map[string]bool, n)
	for i := range n {
		levels[strconv.Itoa(i)] = true
	}
	if !sameKeys(sa.Probabilities, levels) { // check 12
		return fail(ReasonProbabilityKeys, "probability keys %v, want 0..%d", slices.Sorted(maps.Keys(sa.Probabilities)), n-1)
	}
	for k := range sa.Legend { // check 13
		if !levels[k] {
			return fail(ReasonLegendKeys, "legend key %q not in 0..%d", k, n-1)
		}
	}
	if !finite(sa.Score) { // check 14a
		return fail(ReasonNotFinite, "score is %v", sa.Score)
	}
	tol := tolerance(n)
	top := float64(n - 1)
	if sa.Score < -top*tol-eps || sa.Score > top*(1+tol)+eps { // check 14b
		return &AnswerError{Reason: ReasonOutOfRange, scoreRange: true,
			Detail: fmt.Sprintf("score is %v, not within tolerance %v of [0,%d]", sa.Score, tol, n-1)}
	}
	if ae := checkSum(sa.Probabilities, n); ae != nil { // check 15
		return ae
	}
	return nil
}

func sameKeys[V, W any](a map[string]V, b map[string]W) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
