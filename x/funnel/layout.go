package funnel

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/deepnoodle-ai/decide"
)

// Layout says how a stage turns its survivors into requests. Build one with
// PerItem or Shared; the zero Layout is invalid.
type Layout[T any] struct {
	perItem   func(context.Context, Item[T]) (*decide.Request, error)
	questions func(Item[T]) (map[string]decide.Question, error)
	state     any
	size      int
	shared    bool
}

// PerItem sends one request per survivor. build derives the request's state
// from the item and its earlier answers; the request's question keys are the
// item's local names. build runs in a worker slot, possibly concurrently with
// other builds, and must honor ctx.
func PerItem[T any](build func(context.Context, Item[T]) (*decide.Request, error)) Layout[T] {
	return Layout[T]{perItem: build}
}

// Shared asks every survivor's questions against one state. questions
// returns the item's questions by local name and runs once per survivor
// before any request is sent. Requests hold at most size items; 0 puts all
// survivors in one request. Wire keys are "<index>.<local>" and are mapped
// back to local names. Question keys are not sent to the model, so the
// question itself must say which item it is about.
func Shared[T any](state any, questions func(Item[T]) (map[string]decide.Question, error), size int) Layout[T] {
	return Layout[T]{questions: questions, state: state, size: size, shared: true}
}

func (l Layout[T]) check() error {
	switch {
	case !l.shared && l.perItem == nil:
		return fmt.Errorf("%w: layout is zero or has a nil build function", ErrInvalidConfig)
	case l.shared && l.questions == nil:
		return fmt.Errorf("%w: shared layout has a nil questions function", ErrInvalidConfig)
	case l.size < 0:
		return fmt.Errorf("%w: shared layout size is negative", ErrInvalidConfig)
	}
	return nil
}

// batch is one request and the items whose questions it holds. keys maps
// each item's local names to wire keys. For PerItem, req is set by the
// builder inside its worker slot and read only after fanout.Map returns.
type batch struct {
	items []int
	keys  map[int]map[string]string
	req   *decide.Request
}

// plan builds the stage's batches. Items whose questions cannot be built are
// returned in failed; shared items with no questions are returned in empty.
func (l Layout[T]) plan(items []Item[T], survivors []int) (batches []*batch, failed map[int]error, empty []int) {
	failed = make(map[int]error)
	if !l.shared {
		for _, i := range survivors {
			batches = append(batches, &batch{items: []int{i}})
		}
		return batches, failed, nil
	}
	var asked []int
	keys := make(map[int]map[string]string)
	questions := make(map[string]decide.Question)
	for _, i := range survivors {
		qs, err := l.questions(items[i])
		if err == nil {
			err = checkQuestions(qs)
		}
		if err != nil {
			failed[i] = err
			continue
		}
		if len(qs) == 0 {
			empty = append(empty, i)
			continue
		}
		keys[i] = make(map[string]string, len(qs))
		for local, q := range qs {
			wire := strconv.Itoa(i) + "." + local
			keys[i][local] = wire
			questions[wire] = q
		}
		asked = append(asked, i)
	}
	size := l.size
	if size == 0 {
		size = max(len(asked), 1)
	}
	for chunk := range slices.Chunk(asked, size) {
		b := &batch{items: chunk, keys: make(map[int]map[string]string, len(chunk)),
			req: decide.NewRequest(l.state)}
		for _, i := range chunk {
			b.keys[i] = keys[i]
			for _, wire := range keys[i] {
				b.req.Questions[wire] = questions[wire]
			}
		}
		batches = append(batches, b)
	}
	return batches, failed, empty
}

// build returns the request for b, building a PerItem request on demand and
// recording its keys.
func (l Layout[T]) build(ctx context.Context, items []Item[T], b *batch) (*decide.Request, error) {
	if l.shared {
		return b.req, nil
	}
	i := b.items[0]
	req, err := l.perItem(ctx, items[i])
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("funnel: build returned a nil request: %w", decide.ErrInvalidRequest)
	}
	if err := checkQuestions(req.Questions); err != nil {
		return nil, err
	}
	b.req = req
	b.keys = map[int]map[string]string{i: make(map[string]string, len(req.Questions))}
	for key := range req.Questions {
		b.keys[i][key] = key
	}
	return req, nil
}

func checkQuestions(qs map[string]decide.Question) error {
	for _, local := range slices.Sorted(maps.Keys(qs)) {
		if local == "" || nilValue(qs[local]) {
			return fmt.Errorf("funnel: empty question name or nil question %q: %w", local, decide.ErrInvalidRequest)
		}
	}
	return nil
}
