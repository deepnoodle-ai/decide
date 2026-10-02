package rank

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/deepnoodle-ai/decide"
)

// Observation is one Noul and its provenance. Present distinguishes an
// actual zero Noul from a missing result; the zero-value Observation is
// missing. An empty QuestionKey or Model explicitly means that field is
// unknown. Model is the resolved response model ID, not the requested
// alias. Every Observation is revalidated by Rerank or PairwiseSort even
// if it came from a client with validation off.
type Observation struct {
	Present     bool
	Noul        float64
	QuestionKey string
	Model       string
}

// NewObservation makes an explicitly present Noul. Empty questionKey or
// model means unknown provenance. Invalid numbers are rejected here and
// rechecked when an order is built, in case the value was later changed.
func NewObservation(
	noul float64,
	questionKey, model string,
) (Observation, error) {
	o := Observation{
		Present:     true,
		Noul:        noul,
		QuestionKey: questionKey,
		Model:       model,
	}
	if err := checkObservation(o, -1, -1); err != nil {
		return Observation{}, err
	}
	return o, nil
}

// FromResponse reads one Noul from a root response, including the client's
// per-answer validation error when present. It rechecks required wire
// fields and the numeric value through NoulQuestion.ValidateAnswer, since
// root client validation may have been disabled. An empty response model
// is retained as unknown provenance.
func FromResponse(
	resp *decide.Response,
	questionKey string,
) (Observation, error) {
	if questionKey == "" {
		return Observation{}, inputError(
			ErrInvalidInput, -1, -1, "empty question key",
		)
	}
	a, err := decide.AnswerAs[*decide.NoulAnswer](resp, questionKey)
	if err != nil {
		return Observation{}, fmt.Errorf("rank: question %q: %w", questionKey, err)
	}
	if a == nil {
		return Observation{}, inputError(
			ErrInvalidObservation, -1, -1, "nil Noul answer",
		)
	}
	if err := (&decide.NoulQuestion{}).ValidateAnswer(a); err != nil {
		return Observation{}, fmt.Errorf(
			"rank: question %q: %w: %w",
			questionKey,
			ErrInvalidObservation,
			err,
		)
	}
	return NewObservation(a.Noul, questionKey, resp.Model)
}

// Entry is one original item in rank order. Value is the received Noul for
// an independent rerank or the sum of expected wins for a pairwise sort.
// Support is 1 for independent rerank or n-1 for a complete pairwise sort.
type Entry[T any] struct {
	Item    T
	Index   int
	Value   float64
	Support int
}

type Method string

const (
	Independent Method = "independent"
	Pairwise    Method = "pairwise"
)

// Order holds ranked original items and the observations used to produce
// them. Independent observations align with input indices. Pairs retain
// input orientation. ModelIDs contains sorted distinct known model IDs;
// the flags distinguish unknown provenance from a single known model.
type Order[T any] struct {
	Entries            []Entry[T]
	Method             Method
	Observations       []Observation
	Pairs              []PairObservation
	ModelIDs           []string
	HasUnknownModel    bool
	HasUnknownQuestion bool
	HasCycle           bool
}

// Rerank sorts items by descending independent Noul. observations[i]
// belongs to items[i]. secondary is required and breaks equal values by
// ascending key; input index breaks any remaining ties. The question's
// meaning and consistency across candidates are the caller's choice.
func Rerank[T any](
	items []T,
	observations []Observation,
	secondary func(int, T) string,
) (Order[T], error) {
	if secondary == nil {
		return Order[T]{}, inputError(
			ErrInvalidInput, -1, -1, "nil secondary key function",
		)
	}
	if len(items) != len(observations) {
		return Order[T]{}, inputError(
			ErrInvalidInput, -1, -1,
			fmt.Sprintf("%d items but %d observations", len(items), len(observations)),
		)
	}
	out := Order[T]{Method: Independent, Observations: slices.Clone(observations)}
	keys := make([]string, len(items))
	for i, item := range items {
		if err := checkObservation(observations[i], i, -1); err != nil {
			return Order[T]{}, err
		}
		keys[i] = secondary(i, item)
		out.Entries = append(out.Entries, Entry[T]{
			Item:    item,
			Index:   i,
			Value:   observations[i].Noul,
			Support: 1,
		})
	}
	sortEntries(out.Entries, keys)
	setProvenance(&out)
	return out, nil
}

// PairObservation says the first item outranks the second with probability
// Noul. Either pair orientation is accepted; the opposite orientation of
// the same pair is a duplicate, not another vote.
type PairObservation struct {
	First       int
	Second      int
	Observation Observation
}

// PairwiseSort requires every unordered pair exactly once. Each item's
// Value is its sum of expected wins: p for a first item, 1-p for a second
// item. This computed order remains deterministic when preferences cycle;
// HasCycle makes such a cycle visible. It performs no API calls or pair
// scheduling and does not fit a Bradley-Terry scale.
func PairwiseSort[T any](
	items []T,
	pairs []PairObservation,
	secondary func(int, T) string,
) (Order[T], error) {
	if secondary == nil {
		return Order[T]{}, inputError(
			ErrInvalidInput, -1, -1, "nil secondary key function",
		)
	}
	n := len(items)
	expected, ok := pairCount(n)
	if !ok {
		return Order[T]{}, inputError(ErrInvalidInput, -1, -1, "too many items")
	}
	out := Order[T]{Method: Pairwise, Pairs: slices.Clone(pairs)}
	seen := make(map[[2]int]struct{}, len(pairs))
	edges := make([][]int, n)
	values := make([]float64, n)
	for _, pair := range pairs {
		a, b := pair.First, pair.Second
		if a < 0 || b < 0 || a >= n || b >= n || a == b {
			return Order[T]{}, inputError(ErrInvalidInput, a, b, "invalid pair indices")
		}
		if err := checkObservation(pair.Observation, a, b); err != nil {
			return Order[T]{}, err
		}
		key := [2]int{min(a, b), max(a, b)}
		if _, exists := seen[key]; exists {
			return Order[T]{}, inputError(ErrDuplicatePair, a, b, "pair supplied twice")
		}
		seen[key] = struct{}{}
		p := pair.Observation.Noul
		values[a] += p
		values[b] += 1 - p
		switch {
		case p > 0.5:
			edges[a] = append(edges[a], b)
		case p < 0.5:
			edges[b] = append(edges[b], a)
		}
	}
	if len(seen) != expected {
		return Order[T]{}, inputError(ErrIncompletePairs, -1, -1,
			fmt.Sprintf("got %d of %d unordered pairs", len(seen), expected))
	}
	keys := make([]string, n)
	for i, item := range items {
		keys[i] = secondary(i, item)
		out.Entries = append(out.Entries, Entry[T]{
			Item:    item,
			Index:   i,
			Value:   values[i],
			Support: n - 1,
		})
	}
	sortEntries(out.Entries, keys)
	out.HasCycle = hasCycle(edges)
	setProvenance(&out)
	return out, nil
}

// Selection is the longest rank prefix within an allowance. FirstExcluded
// is the position in Order.Entries that did not fit, or -1 if all fit.
type Selection[T any] struct {
	Entries       []Entry[T]
	Used          uint64
	FirstExcluded int
}

// SelectUnderBudget takes a prefix of an existing order. size supplies a
// nonnegative integer cost in the caller's unit (for example, tokens).
// It stops at the first item that will not fit, even if later items would.
func SelectUnderBudget[T any](
	order Order[T],
	allowance uint64,
	size func(T) uint64,
) (Selection[T], error) {
	if size == nil {
		return Selection[T]{}, inputError(
			ErrInvalidInput, -1, -1, "nil size function",
		)
	}
	out := Selection[T]{FirstExcluded: -1}
	for i, entry := range order.Entries {
		n := size(entry.Item)
		if n > allowance-out.Used {
			out.FirstExcluded = i
			break
		}
		out.Used += n
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

func checkObservation(o Observation, index, other int) error {
	if !o.Present {
		return inputError(
			ErrInvalidObservation, index, other, "missing Noul observation",
		)
	}
	if math.IsNaN(o.Noul) || math.IsInf(o.Noul, 0) || o.Noul < 0 || o.Noul > 1 {
		return inputError(
			ErrInvalidObservation, index, other, "Noul must be finite and in [0,1]",
		)
	}
	return nil
}

func sortEntries[T any](entries []Entry[T], keys []string) {
	slices.SortFunc(entries, func(a, b Entry[T]) int {
		if c := cmp.Compare(b.Value, a.Value); c != 0 {
			return c
		}
		if c := cmp.Compare(keys[a.Index], keys[b.Index]); c != 0 {
			return c
		}
		return cmp.Compare(a.Index, b.Index)
	})
}

func pairCount(n int) (int, bool) {
	if n < 2 {
		return 0, true
	}
	a, b := n, n-1
	if a%2 == 0 {
		a /= 2
	} else {
		b /= 2
	}
	if a > math.MaxInt/b {
		return 0, false
	}
	return a * b, true
}

func hasCycle(edges [][]int) bool {
	state := make([]uint8, len(edges))
	var visit func(int) bool
	visit = func(i int) bool {
		state[i] = 1
		for _, j := range edges[i] {
			if state[j] == 1 || state[j] == 0 && visit(j) {
				return true
			}
		}
		state[i] = 2
		return false
	}
	for i := range edges {
		if state[i] == 0 && visit(i) {
			return true
		}
	}
	return false
}

func setProvenance[T any](out *Order[T]) {
	models := make(map[string]struct{})
	add := func(o Observation) {
		if o.Model == "" {
			out.HasUnknownModel = true
		} else {
			models[o.Model] = struct{}{}
		}
		if o.QuestionKey == "" {
			out.HasUnknownQuestion = true
		}
	}
	for _, o := range out.Observations {
		add(o)
	}
	for _, p := range out.Pairs {
		add(p.Observation)
	}
	for model := range models {
		out.ModelIDs = append(out.ModelIDs, model)
	}
	slices.Sort(out.ModelIDs)
}
