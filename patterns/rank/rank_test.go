package rank_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/patterns/rank"
)

func tie(_ int, s string) string { return s }

func present(p float64) rank.Observation {
	return rank.Observation{Present: true, Noul: p}
}

func pair(first, second int, p float64) rank.PairObservation {
	return rank.PairObservation{
		First:       first,
		Second:      second,
		Observation: present(p),
	}
}

func responseOf(a decide.Answer) *decide.Response {
	return &decide.Response{
		Answers: map[string]decide.Answer{"relevant": a},
	}
}

func indices[T any](entries []rank.Entry[T]) []int {
	out := make([]int, len(entries))
	for i, e := range entries {
		out[i] = e.Index
	}
	return out
}

func TestRerank(t *testing.T) {
	items := []string{"z", "a", "a", "b"}
	observations := []rank.Observation{
		{Present: true, Noul: 0.5, Model: "jev-a", QuestionKey: "relevant"},
		{Present: true, Noul: 0.8, Model: "jev-b", QuestionKey: "relevant"},
		{Present: true, Noul: 0.8},
		{Present: true, Noul: 0.8, Model: "jev-a", QuestionKey: "relevant"},
	}
	got, err := rank.Rerank(items, observations, tie)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 0}; !slices.Equal(indices(got.Entries), want) {
		t.Fatalf("order %v, want %v", indices(got.Entries), want)
	}
	if got.Entries[0].Item != "a" || got.Entries[1].Item != "a" {
		t.Fatalf("duplicate items not retained: %+v", got.Entries)
	}
	if !slices.Equal(got.ModelIDs, []string{"jev-a", "jev-b"}) ||
		!got.HasUnknownModel || !got.HasUnknownQuestion {
		t.Fatalf("provenance lost: %+v", got)
	}
	if got.Observations[2].Noul != 0.8 || got.Entries[2].Support != 1 {
		t.Fatalf("raw observation/support lost: %+v", got)
	}
	// The result owns its observation slice, not the caller's slice.
	observations[2].Noul = 0
	if got.Observations[2].Noul != 0.8 {
		t.Fatal("result aliases input observation slice")
	}
}

func TestNewObservationZero(t *testing.T) {
	o, err := rank.NewObservation(0, "", "")
	if err != nil || !o.Present || o.Noul != 0 {
		t.Fatalf("zero Noul must be present: %+v, %v", o, err)
	}
	_, err = rank.NewObservation(math.NaN(), "q", "jev-a")
	if !errors.Is(err, rank.ErrInvalidObservation) {
		t.Fatalf("invalid direct observation: %v", err)
	}
}

func TestRerankRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		obs  []rank.Observation
		key  func(int, string) string
		want error
	}{
		{"count", nil, tie, rank.ErrInvalidInput},
		{"nil key", []rank.Observation{present(0.5)}, nil, rank.ErrInvalidInput},
		{"missing", []rank.Observation{{}}, tie, rank.ErrInvalidObservation},
		{
			"nan", []rank.Observation{present(math.NaN())},
			tie, rank.ErrInvalidObservation,
		},
		{
			"infinity", []rank.Observation{present(math.Inf(1))},
			tie, rank.ErrInvalidObservation,
		},
		{
			"negative", []rank.Observation{present(-0.01)},
			tie, rank.ErrInvalidObservation,
		},
		{
			"above one", []rank.Observation{present(1.01)},
			tie, rank.ErrInvalidObservation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rank.Rerank([]string{"a"}, tc.obs, tc.key)
			if !errors.Is(err, tc.want) || len(got.Entries) != 0 {
				t.Fatalf("got %+v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestPairwiseSort(t *testing.T) {
	items := []string{"c", "a", "b"}
	// A > B > C. The A/B observation uses reverse orientation.
	pairs := []rank.PairObservation{
		{First: 0, Second: 1, Observation: rank.Observation{
			Present: true, Noul: 0.25, Model: "jev-a",
		}},
		{First: 0, Second: 2, Observation: rank.Observation{
			Present: true, Noul: 0.25, Model: "jev-a",
		}},
		{First: 2, Second: 1, Observation: rank.Observation{
			Present: true, Noul: 0.25, Model: "jev-a",
		}},
	}
	got, err := rank.PairwiseSort(items, pairs, tie)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 0}; !slices.Equal(indices(got.Entries), want) {
		t.Fatalf("order %v, want %v", indices(got.Entries), want)
	}
	if got.HasCycle || got.Entries[0].Value != 1.5 || got.Entries[0].Support != 2 {
		t.Fatalf("score, support, or cycle wrong: %+v", got)
	}
	if !slices.Equal(got.ModelIDs, []string{"jev-a"}) || !got.HasUnknownQuestion {
		t.Fatalf("provenance wrong: %+v", got)
	}
}

func TestPairwiseCycleAndTie(t *testing.T) {
	items := []string{"c", "a", "b"}
	cycle := []rank.PairObservation{
		pair(0, 1, 0.75),
		pair(1, 2, 0.75),
		pair(2, 0, 0.75),
	}
	got, err := rank.PairwiseSort(items, cycle, tie)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasCycle || !slices.Equal(indices(got.Entries), []int{1, 2, 0}) {
		t.Fatalf("cycle or tie order wrong: %+v", got)
	}
	for _, entry := range got.Entries {
		if entry.Value != 1 || entry.Support != 2 {
			t.Fatalf("cycle score/support wrong: %+v", entry)
		}
	}
	allTies := slices.Clone(cycle)
	for i := range allTies {
		allTies[i].Observation.Noul = 0.5
	}
	got, err = rank.PairwiseSort(items, allTies, tie)
	if err != nil || got.HasCycle {
		t.Fatalf("0.5 ties should have no edges: %+v, %v", got, err)
	}
}

func TestPairwiseRejectsIncompleteAndDuplicate(t *testing.T) {
	valid := pair(0, 1, 0.5)
	for _, tc := range []struct {
		name  string
		pairs []rank.PairObservation
		want  error
	}{
		{"missing", nil, rank.ErrIncompletePairs},
		{
			"duplicate reverse",
			[]rank.PairObservation{valid, pair(1, 0, 0.5)},
			rank.ErrDuplicatePair,
		},
		{"bad index", []rank.PairObservation{pair(0, 3, 0.5)}, rank.ErrInvalidInput},
		{"self pair", []rank.PairObservation{pair(0, 0, 0.5)}, rank.ErrInvalidInput},
		{
			"bad value", []rank.PairObservation{pair(0, 1, math.NaN())},
			rank.ErrInvalidObservation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rank.PairwiseSort([]string{"a", "b"}, tc.pairs, tie)
			if !errors.Is(err, tc.want) || len(got.Entries) != 0 {
				t.Fatalf("got %+v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestFromResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp *decide.Response
		key  string
		want error
	}{
		{"missing", &decide.Response{}, "relevant", decide.ErrInvalidAnswer},
		{
			"wrong type", responseOf(&decide.ChoiceAnswer{}),
			"relevant", decide.ErrInvalidAnswer,
		},
		{
			"typed nil", responseOf((*decide.NoulAnswer)(nil)),
			"relevant", rank.ErrInvalidObservation,
		},
		{
			"bad raw value",
			responseOf(&decide.NoulAnswer{Noul: math.Inf(-1)}),
			"relevant", rank.ErrInvalidObservation,
		},
		{"empty key", &decide.Response{}, "", rank.ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rank.FromResponse(tc.resp, tc.key)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	resp := &decide.Response{
		Model: "jev-1.13.0",
		Answers: map[string]decide.Answer{
			"relevant": &decide.NoulAnswer{Noul: 0.7},
		},
	}
	got, err := rank.FromResponse(resp, "relevant")
	if err != nil || got.Noul != 0.7 ||
		got.Model != resp.Model || got.QuestionKey != "relevant" {
		t.Fatalf("provenance wrong: %+v, %v", got, err)
	}
	resp.Invalid = map[string]*decide.AnswerError{
		"relevant": {
			Key: "relevant", Type: "noul",
			Reason: decide.ReasonOutOfRange,
		},
	}
	if _, err := rank.FromResponse(resp, "relevant"); err == nil {
		t.Fatal("ignored Response.Invalid")
	}
}

func TestFromResponseWithFake(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("relevant", decidetest.NoulAnswer(0.72))
	client := srv.Client(t)
	req := decide.NewRequest(map[string]any{
		"query":     "where is the policy?",
		"candidate": "policy text",
	})
	decide.Ask(
		req, "relevant",
		decide.Noul("Does this candidate answer the query?"),
	)
	resp, err := client.SystemOne(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	o, err := rank.FromResponse(resp, "relevant")
	if err != nil || o.Noul != 0.72 || o.Model != "jev-1.13.0" {
		t.Fatalf("fake response: %+v, %v", o, err)
	}
	srv.Answer("relevant", decidetest.NoulAnswer(1.25))
	resp, err = client.SystemOne(
		context.Background(), req, decide.WithCallValidation(false),
	)
	if err != nil || len(resp.Invalid) != 0 {
		t.Fatalf("expected raw unvalidated response: %v, %+v", err, resp)
	}
	_, err = rank.FromResponse(resp, "relevant")
	if !errors.Is(err, rank.ErrInvalidObservation) {
		t.Fatalf("raw invalid Noul was accepted: %v", err)
	}
	for _, tc := range []struct {
		name string
		wire json.RawMessage
	}{
		{"missing", json.RawMessage(`{"type":"noul"}`)},
		{"null", json.RawMessage(`{"type":"noul","noul":null}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.Respond(func(*decide.Request) (*decide.Response, error) {
				return &decide.Response{
					Answers: map[string]decide.Answer{
						"relevant": &decide.RawAnswer{
							Type: "noul", JSON: tc.wire,
						},
					},
				}, nil
			})
			resp, err := client.SystemOne(
				context.Background(), req,
				decide.WithCallValidation(false),
			)
			if err != nil || len(resp.Invalid) != 0 {
				t.Fatalf("expected unvalidated response: %v", err)
			}
			_, err = rank.FromResponse(resp, "relevant")
			var ae *decide.AnswerError
			if !errors.As(err, &ae) ||
				ae.Reason != decide.ReasonMissingField {
				t.Fatalf("missing Noul was accepted: %v", err)
			}
		})
	}
}

func TestSelectUnderBudget(t *testing.T) {
	order, err := rank.Rerank(
		[]string{"aaaa", "bbb", "c"},
		[]rank.Observation{present(0.9), present(0.8), present(0.7)},
		tie,
	)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := rank.SelectUnderBudget(
		order, 5, func(s string) uint64 { return uint64(len(s)) },
	)
	if err != nil || selected.Used != 4 || selected.FirstExcluded != 1 ||
		!slices.Equal(indices(selected.Entries), []int{0}) {
		t.Fatalf("prefix selection: %+v, %v", selected, err)
	}
	selected, err = rank.SelectUnderBudget(
		order, 0, func(string) uint64 { return 0 },
	)
	if err != nil || selected.Used != 0 ||
		selected.FirstExcluded != -1 || len(selected.Entries) != 3 {
		t.Fatalf("zero-size selection: %+v, %v", selected, err)
	}
	selected, err = rank.SelectUnderBudget(
		order, math.MaxUint64,
		func(string) uint64 { return math.MaxUint64 },
	)
	if err != nil || selected.Used != math.MaxUint64 ||
		selected.FirstExcluded != 1 {
		t.Fatalf("overflow guard: %+v, %v", selected, err)
	}
	_, err = rank.SelectUnderBudget(order, 5, nil)
	if !errors.Is(err, rank.ErrInvalidInput) {
		t.Fatalf("nil size function: %v", err)
	}
}
