package compact

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/patterns/fanout"
)

// Default question wording. The segment and its short form are sent beside
// the question in a JSON object; the focus state says what the context is
// for.
const (
	DefaultNeeded   = "Given the state, will this segment still be needed to continue the work?"
	DefaultVerbatim = "Given the state, is the exact full text of this segment needed, rather than its short form?"
)

// Limits are request size limits in estimated tokens: the complete serialized
// request, including JSON framing and the client's model, and the state plus
// the longest question.
type Limits struct {
	Request          int
	StateAndQuestion int
}

// Jev113 holds the documented Jev 1.13 limits. They can change without
// notice; see https://docs.typesafe.ai/models.
var Jev113 = Limits{Request: 64_000, StateAndQuestion: 32_000}

// Config configures Compact. Focus is the request state: what the kept
// context is for, such as the task and the latest turns. Workers bounds
// concurrent requests. Estimate returns an upper-bound token count for a
// string; the nil default is its byte length, which assumes no token is
// shorter than one byte.
type Config struct {
	Focus    any
	Rule     Rule
	Workers  int
	Needed   string
	Verbatim string
	Limits   Limits
	Estimate func(string) int
}

// Result holds one decision per segment in input order. Before is the total
// size of every segment and After the total size kept. Requests, tokens as
// received, and sorted distinct resolved models describe the calls made.
type Result struct {
	Decisions    []Decision
	Before       int
	After        int
	Requests     int
	InputTokens  int
	OutputTokens int
	Models       []string
}

// Apply returns the kept texts in input order, with the short form for a
// segment whose action is Short. segs must be the slice passed to Compact.
func (r *Result) Apply(segs []Segment) []string {
	var out []string
	for _, d := range r.Decisions {
		switch d.Action {
		case Keep:
			out = append(out, segs[d.Index].Text)
		case Short:
			out = append(out, segs[d.Index].Short)
		}
	}
	return out
}

// Compact scores every unpinned segment against cfg.Focus, packing the
// questions into requests under cfg.Limits, then calls Select. A segment
// that cannot be asked about or gets an invalid answer is unscored and kept
// ahead of scored ones if it fits. A failed request or cancellation fails the
// call with a nil result. ErrOverBudget is returned with the result; when the
// pinned sizes alone exceed the budget, no request is sent and every unpinned
// segment is dropped unscored.
func Compact(ctx context.Context, client *decide.Client, segs []Segment, cfg Config) (*Result, error) {
	cfg.Needed = cmp.Or(cfg.Needed, DefaultNeeded)
	cfg.Verbatim = cmp.Or(cfg.Verbatim, DefaultVerbatim)
	if cfg.Limits == (Limits{}) {
		cfg.Limits = Jev113
	}
	if cfg.Estimate == nil {
		cfg.Estimate = func(s string) int { return len(s) }
	}
	switch {
	case ctx == nil || client == nil:
		return nil, fmt.Errorf("%w: nil context or client", ErrInvalidInput)
	case cfg.Workers < 1:
		return nil, fmt.Errorf("%w: workers must be positive", ErrInvalidInput)
	case cfg.Limits.Request <= 0 || cfg.Limits.StateAndQuestion <= 0:
		return nil, fmt.Errorf("%w: limits must be positive", ErrInvalidInput)
	}
	if err := checkRule(cfg.Rule); err != nil {
		return nil, err
	}
	if err := checkSegments(segs); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	batches, scores, err := pack(segs, cfg, client.DefaultModel())
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	res := &Result{}
	pinned := 0
	for _, s := range segs {
		if s.Pinned {
			pinned += s.Size
		}
	}
	if pinned > cfg.Rule.Budget {
		for i, s := range segs {
			if !s.Pinned {
				scores[i].Err = fmt.Errorf("compact: segment %d not scored: %w", i, ErrOverBudget)
			}
		}
		return res.decide(segs, scores, cfg.Rule)
	}
	results, err := fanout.Map(ctx, client, batches, cfg.Workers,
		func(_ context.Context, _ int, b *batch) (*decide.Request, error) { return b.req, nil })
	if err != nil {
		return nil, err
	}

	models := make(map[string]bool)
	for n, r := range results {
		if r.Err != nil && !errors.Is(r.Err, decide.ErrInvalidAnswer) || r.Response == nil {
			return nil, fmt.Errorf("compact: request %d: %w", n, cmp.Or(r.Err, errors.New("no response")))
		}
		res.Requests++
		res.InputTokens += r.Response.Usage.InputTokens
		res.OutputTokens += r.Response.Usage.OutputTokens
		if r.Response.Model != "" {
			models[r.Response.Model] = true
		}
		for _, i := range batches[n].segs {
			scores[i] = read(batches[n].req, r.Response, i)
		}
	}
	res.Models = slices.Sorted(maps.Keys(models))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return res.decide(segs, scores, cfg.Rule)
}

// decide runs Select and fills in the sizes before and after.
func (res *Result) decide(segs []Segment, scores []Scores, rule Rule) (*Result, error) {
	var err error
	res.Decisions, err = Select(segs, scores, rule)
	for _, s := range segs {
		res.Before += s.Size
	}
	for _, d := range res.Decisions {
		switch d.Action {
		case Keep:
			res.After += segs[d.Index].Size
		case Short:
			res.After += segs[d.Index].ShortSize
		}
	}
	return res, err
}

type batch struct {
	req  *decide.Request
	segs []int
}

func neededKey(i int) string   { return strconv.Itoa(i) + ".needed" }
func verbatimKey(i int) string { return strconv.Itoa(i) + ".verbatim" }

// pack builds the requests. Segments too large to ask about get an unscored
// ErrTooLarge in scores and are not sent.
func pack(segs []Segment, cfg Config, model string) ([]*batch, []Scores, error) {
	scores := make([]Scores, len(segs))
	stateJSON, err := json.Marshal(cfg.Focus)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: focus: %w", ErrInvalidInput, err)
	}
	state := cfg.Estimate(string(stateJSON))
	if state > cfg.Limits.StateAndQuestion {
		return nil, nil, fmt.Errorf("%w: focus state is about %d tokens", ErrTooLarge, state)
	}

	var batches []*batch
	var cur *batch
	requestSize := func(req *decide.Request) (int, error) {
		b, err := json.Marshal(req)
		if err != nil {
			return 0, fmt.Errorf("compact: encode request: %w", err)
		}
		return cfg.Estimate(string(b)), nil
	}
	for i, s := range segs {
		if s.Pinned {
			continue
		}
		qs := map[string]decide.Question{
			neededKey(i): decide.Noul(map[string]string{"question": cfg.Needed, "segment": s.Text}),
		}
		if cfg.Rule.ShortBelow > 0 && s.Short != "" {
			qs[verbatimKey(i)] = decide.Noul(map[string]string{
				"question": cfg.Verbatim, "segment": s.Text, "short_form": s.Short})
		}
		longest := 0
		for key, q := range qs {
			b, err := json.Marshal(q)
			if err != nil {
				return nil, nil, fmt.Errorf("compact: segment %d: %w", i, err)
			}
			c := cfg.Estimate(strconv.Quote(key) + ":" + string(b))
			longest = max(longest, c)
		}
		// Freeze the serialized focus and model so packing counts the same
		// request fields that the client sends.
		req := decide.NewRequest(json.RawMessage(stateJSON), decide.WithRequestModel(model))
		maps.Copy(req.Questions, qs)
		cost, err := requestSize(req)
		if err != nil {
			return nil, nil, err
		}
		if state+longest > cfg.Limits.StateAndQuestion || cost > cfg.Limits.Request {
			scores[i].Err = fmt.Errorf("%w: segment %d request is about %d tokens", ErrTooLarge, i, cost)
			continue
		}
		if cur != nil {
			maps.Copy(cur.req.Questions, qs)
			cost, err = requestSize(cur.req)
			if err != nil {
				return nil, nil, err
			}
			if cost > cfg.Limits.Request {
				for key := range qs {
					delete(cur.req.Questions, key)
				}
				cur = nil
			}
		}
		if cur == nil {
			cur = &batch{req: req}
			batches = append(batches, cur)
		}
		cur.segs = append(cur.segs, i)
	}
	return batches, scores, nil
}

// read returns segment i's scores from resp, or an unscored error.
func read(req *decide.Request, resp *decide.Response, i int) Scores {
	var s Scores
	for _, key := range []string{neededKey(i), verbatimKey(i)} {
		q, ok := req.Questions[key]
		if !ok {
			continue
		}
		a := resp.Answers[key]
		err := validate(key, q, a)
		if err == nil && resp.Invalid[key] != nil {
			err = resp.Invalid[key]
		}
		if err != nil {
			return Scores{Err: fmt.Errorf("compact: segment %d: %w", i, err)}
		}
		// The validator tolerates serialization noise just outside [0,1],
		// which Select rejects, so clamp what it accepted.
		p := min(max(a.(*decide.NoulAnswer).Noul, 0), 1)
		if key == neededKey(i) {
			s.Needed = p
		} else {
			s.Verbatim, s.HasVerbatim = p, true
		}
	}
	return s
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
	if _, ok := a.(*decide.NoulAnswer); !ok {
		return &decide.AnswerError{Key: key, Type: typ,
			Reason: decide.ReasonTypeMismatch,
			Detail: fmt.Sprintf("answer type %q, question type %q", a.AnswerType(), typ)}
	}
	if err := q.(*decide.NoulQuestion).ValidateAnswer(a); err != nil {
		if ae, ok := errors.AsType[*decide.AnswerError](err); ok {
			copyErr := *ae
			copyErr.Key = key
			return &copyErr
		}
		return &decide.AnswerError{Key: key, Type: typ, Reason: decide.ReasonCustom, Err: err}
	}
	return nil
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
