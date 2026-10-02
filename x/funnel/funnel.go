package funnel

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/x/fanout"
)

// ErrInvalidConfig reports an invalid Run argument or stage.
var ErrInvalidConfig = errors.New("funnel: invalid configuration")

// Stage is one screening step. Ask builds its requests. Keep, if set,
// decides whether an answered item survives; nil keeps every answered item.
// With Limit > 0, at most Limit kept items survive, highest Rank first and
// input order on ties. The package ships no thresholds.
type Stage[T any] struct {
	Name  string
	Ask   Layout[T]
	Keep  func(Item[T]) (bool, error)
	Rank  func(Item[T]) (float64, error)
	Limit int
}

// Item is one input and what the run learned about it. Answers holds valid
// answers by stage name and local question name. Drop is nil for an item
// that survived every stage that ran.
type Item[T any] struct {
	Index   int
	Value   T
	Answers map[string]map[string]decide.Answer
	Drop    *Drop
}

// Cause says why an item left the funnel.
type Cause string

const (
	CauseKeep  Cause = "keep"  // Keep returned false
	CauseLimit Cause = "limit" // ranked past the stage's Limit
	CauseError Cause = "error" // build, request, answer, Keep, Rank, or cancellation
)

// Drop records the stage an item left at and why. Err is set for
// CauseError.
type Drop struct {
	Stage string
	Cause Cause
	Err   error
}

// Report summarizes one stage. In counts items entering it, Out items
// leaving it alive, and Failed items dropped with CauseError. Responses
// counts responses received, including any returned with an answer
// validation error; tokens are summed from those responses as received.
// Models lists distinct non-empty resolved model IDs, sorted.
type Report struct {
	Name         string
	In           int
	Out          int
	Failed       int
	Responses    int
	InputTokens  int
	OutputTokens int
	Models       []string
}

// Result holds every input item in input order and one report per stage.
type Result[T any] struct {
	Items  []Item[T]
	Stages []Report
}

// Survivors returns the items that were not dropped, in input order.
func (r *Result[T]) Survivors() []Item[T] {
	var out []Item[T]
	for _, it := range r.Items {
		if it.Drop == nil {
			out = append(out, it)
		}
	}
	return out
}

// Run sends each stage's requests for the items that survived the earlier
// stages, at most workers at a time, through client. Item errors drop only
// the items they touch and never become the returned error. Configuration
// errors return a nil result and wrap ErrInvalidConfig. On cancellation, Run
// keeps completed answers, drops unfinished items with the context error,
// runs no further stages, and returns the result with an error matching
// ctx.Err(). Keep and Rank run on the calling goroutine and must not mutate
// the item's Answers.
func Run[T any](
	ctx context.Context,
	client *decide.Client,
	items []T,
	workers int,
	stages ...Stage[T],
) (*Result[T], error) {
	if err := checkConfig(ctx, client, workers, stages); err != nil {
		return nil, err
	}
	res := &Result[T]{Items: make([]Item[T], len(items))}
	for i, v := range items {
		res.Items[i] = Item[T]{Index: i, Value: v, Answers: map[string]map[string]decide.Answer{}}
	}
	for _, st := range stages {
		report := Report{Name: st.Name}
		if err := ctx.Err(); err != nil {
			for i := range res.Items {
				if res.Items[i].Drop == nil {
					res.Items[i].Drop = &Drop{Stage: st.Name, Cause: CauseError, Err: err}
					report.In++
					report.Failed++
				}
			}
			res.Stages = append(res.Stages, report)
			continue
		}
		runStage(ctx, client, workers, st, res.Items, &report)
		res.Stages = append(res.Stages, report)
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	return res, nil
}

func checkConfig[T any](ctx context.Context, client *decide.Client, workers int, stages []Stage[T]) error {
	switch {
	case ctx == nil:
		return fmt.Errorf("%w: context is nil", ErrInvalidConfig)
	case client == nil:
		return fmt.Errorf("%w: client is nil", ErrInvalidConfig)
	case workers < 1:
		return fmt.Errorf("%w: workers must be positive", ErrInvalidConfig)
	case len(stages) == 0:
		return fmt.Errorf("%w: no stages", ErrInvalidConfig)
	}
	names := make(map[string]bool, len(stages))
	for _, st := range stages {
		if st.Name == "" || names[st.Name] {
			return fmt.Errorf("%w: stage name %q is empty or repeated", ErrInvalidConfig, st.Name)
		}
		names[st.Name] = true
		if err := st.Ask.check(); err != nil {
			return fmt.Errorf("stage %q: %w", st.Name, err)
		}
		if st.Limit < 0 || (st.Limit > 0 && st.Rank == nil) {
			return fmt.Errorf("%w: stage %q needs Limit >= 0 and a Rank when Limit > 0", ErrInvalidConfig, st.Name)
		}
	}
	return nil
}

func runStage[T any](
	ctx context.Context,
	client *decide.Client,
	workers int,
	st Stage[T],
	items []Item[T],
	report *Report,
) {
	var survivors []int
	for i := range items {
		if items[i].Drop == nil {
			survivors = append(survivors, i)
		}
	}
	report.In = len(survivors)
	if len(survivors) == 0 {
		return
	}
	drop := func(i int, cause Cause, err error) {
		items[i].Drop = &Drop{Stage: st.Name, Cause: cause, Err: err}
		if cause == CauseError {
			report.Failed++
		}
	}

	batches, failed, answered := st.Ask.plan(items, survivors)
	// Shared items with no questions still complete the stage and reach Keep.
	for _, i := range answered {
		items[i].Answers[st.Name] = make(map[string]decide.Answer)
	}
	for _, i := range survivors {
		if err := failed[i]; err != nil {
			drop(i, CauseError, fmt.Errorf("funnel: stage %q questions: %w", st.Name, err))
		}
	}
	// A context error here is also recorded in every unstarted result, so
	// the stage handles it per batch below.
	results, _ := fanout.Map(ctx, client, batches, workers,
		func(ctx context.Context, _ int, b *batch) (*decide.Request, error) {
			return st.Ask.build(ctx, items, b)
		})

	models := make(map[string]bool)
	for n, r := range results {
		b := batches[n]
		if r.Response != nil {
			report.Responses++
			report.InputTokens += r.Response.Usage.InputTokens
			report.OutputTokens += r.Response.Usage.OutputTokens
			if r.Response.Model != "" {
				models[r.Response.Model] = true
			}
		}
		if r.Response == nil || (r.Err != nil && !errors.Is(r.Err, decide.ErrInvalidAnswer)) {
			err := r.Err
			if err == nil {
				err = errors.New("no response")
			}
			for _, i := range b.items {
				drop(i, CauseError, fmt.Errorf("funnel: stage %q request: %w", st.Name, err))
			}
			continue
		}
		for _, i := range b.items {
			answers, err := readItem(b, i, r.Response)
			if err != nil {
				drop(i, CauseError, fmt.Errorf("funnel: stage %q %w", st.Name, err))
				continue
			}
			items[i].Answers[st.Name] = answers
			answered = append(answered, i)
		}
	}
	report.Models = slices.Sorted(maps.Keys(models))
	slices.Sort(answered)

	var kept []int
	for _, i := range answered {
		if st.Keep == nil {
			kept = append(kept, i)
			continue
		}
		ok, err := st.Keep(items[i])
		switch {
		case err != nil:
			drop(i, CauseError, fmt.Errorf("funnel: stage %q keep: %w", st.Name, err))
		case !ok:
			drop(i, CauseKeep, nil)
		default:
			kept = append(kept, i)
		}
	}

	if st.Limit > 0 && len(kept) > st.Limit {
		type ranked struct {
			i    int
			rank float64
		}
		var rs []ranked
		for _, i := range kept {
			v, err := st.Rank(items[i])
			if err == nil && math.IsNaN(v) {
				err = errors.New("rank is NaN")
			}
			if err != nil {
				drop(i, CauseError, fmt.Errorf("funnel: stage %q rank: %w", st.Name, err))
				continue
			}
			rs = append(rs, ranked{i, v})
		}
		slices.SortStableFunc(rs, func(a, b ranked) int {
			return cmp.Or(cmp.Compare(b.rank, a.rank), cmp.Compare(a.i, b.i))
		})
		for _, r := range rs[min(st.Limit, len(rs)):] {
			drop(r.i, CauseLimit, nil)
		}
	}

	for _, i := range survivors {
		if items[i].Drop == nil {
			report.Out++
		}
	}
}

// readItem checks and returns item i's answers by local name. The first
// failure by sorted local name is returned.
func readItem(b *batch, i int, resp *decide.Response) (map[string]decide.Answer, error) {
	keys := b.keys[i]
	out := make(map[string]decide.Answer, len(keys))
	for _, local := range slices.Sorted(maps.Keys(keys)) {
		wire := keys[local]
		a := resp.Answers[wire]
		if err := validate(wire, b.req.Questions[wire], a); err != nil {
			return nil, fmt.Errorf("question %q: %w", local, err)
		}
		if ae := resp.Invalid[wire]; ae != nil {
			return nil, fmt.Errorf("question %q: %w", local, ae)
		}
		out[local] = a
	}
	return out, nil
}

func validate(key string, q decide.Question, a decide.Answer) error {
	typ := q.QuestionType()
	if nilValue(a) {
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonMissingAnswer, Detail: "no answer for question"}
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
	v, ok := q.(decide.AnswerValidator)
	if !ok {
		return nil
	}
	if err := v.ValidateAnswer(a); err != nil {
		if ae, ok := errors.AsType[*decide.AnswerError](err); ok {
			copyErr := *ae
			copyErr.Key = key
			copyErr.Type = cmp.Or(copyErr.Type, typ)
			return &copyErr
		}
		return &decide.AnswerError{Key: key, Type: typ, Reason: decide.ReasonCustom, Err: err}
	}
	return nil
}

// AnswerAs reads one of an item's answers as A. A missing answer or a
// different Go type is an error naming the stage and local question.
func AnswerAs[A decide.Answer, T any](it Item[T], stage, local string) (A, error) {
	var zero A
	a, ok := it.Answers[stage][local]
	if !ok || nilValue(a) {
		return zero, fmt.Errorf("funnel: item %d has no answer for stage %q question %q", it.Index, stage, local)
	}
	typed, ok := a.(A)
	if !ok {
		return zero, fmt.Errorf("funnel: item %d stage %q question %q: answer is %T, want %T", it.Index, stage, local, a, zero)
	}
	return typed, nil
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
