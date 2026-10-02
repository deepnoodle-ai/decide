package compact_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/compact"
)

func TestCompactCountsCompleteWireRequest(t *testing.T) {
	model := strings.Repeat("configured-model-", 20)
	request := decide.NewRequest("f", decide.WithRequestModel(model))
	request.Questions["0.needed"] = decide.Noul(map[string]string{"question": "?", "segment": "one"})
	wire, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("rejects envelope overflow", func(t *testing.T) {
		srv := decidetest.NewServer(t)
		res, err := compact.Compact(context.Background(), srv.Client(t, decide.WithModel(model)),
			[]compact.Segment{{Text: "one", Size: 1}}, compact.Config{
				Focus: "f", Needed: "?", Rule: compact.Rule{Budget: 10}, Workers: 1,
				Limits: compact.Limits{Request: len(wire) - 1, StateAndQuestion: 1000},
			})
		if err != nil {
			t.Fatal(err)
		}
		if n := len(srv.Requests()); n != 0 {
			t.Fatalf("sent %d requests whose envelope exceeds the limit", n)
		}
		if !errors.Is(res.Decisions[0].Scores.Err, compact.ErrTooLarge) {
			t.Fatalf("oversize segment = %+v", res.Decisions[0])
		}
	})
	t.Run("splits before envelope overflow", func(t *testing.T) {
		request.Questions["1.needed"] = decide.Noul(map[string]string{"question": "?", "segment": "two"})
		two, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		limit := len(two) - 1
		srv := decidetest.NewServer(t)
		res, err := compact.Compact(context.Background(), srv.Client(t, decide.WithModel(model)),
			[]compact.Segment{{Text: "one", Size: 1}, {Text: "two", Size: 1}}, compact.Config{
				Focus: "f", Needed: "?", Rule: compact.Rule{Budget: 10}, Workers: 1,
				Limits: compact.Limits{Request: limit, StateAndQuestion: 1000},
			})
		if err != nil {
			t.Fatal(err)
		}
		if res.Requests != 2 || len(srv.Requests()) != 2 {
			t.Fatalf("requests = %d, want two bounded requests", res.Requests)
		}
		for _, recorded := range srv.Requests() {
			b, err := json.Marshal(recorded.Request)
			if err != nil {
				t.Fatal(err)
			}
			if len(b) > limit || recorded.Request.Model != model {
				t.Fatalf("wire size = %d, limit = %d, model = %q", len(b), limit, recorded.Request.Model)
			}
		}
	})
}

func TestCompactCanceledLocalPaths(t *testing.T) {
	for name, segs := range map[string][]compact.Segment{
		"empty":              nil,
		"pinned":             {{Text: "pinned", Size: 1, Pinned: true}},
		"pinned over budget": {{Text: "pinned", Size: 2, Pinned: true}},
	} {
		t.Run(name, func(t *testing.T) {
			srv := decidetest.NewServer(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			res, err := compact.Compact(ctx, srv.Client(t), segs,
				compact.Config{Focus: "f", Rule: compact.Rule{Budget: 1}, Workers: 1})
			if res != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("result = %+v, error = %v", res, err)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Fatalf("sent %d requests after cancellation", n)
			}
		})
	}
}

func TestCompactChecksCancellationAfterPacking(t *testing.T) {
	srv := decidetest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, err := compact.Compact(ctx, srv.Client(t), nil,
		compact.Config{Focus: "f", Rule: compact.Rule{Budget: 1}, Workers: 1,
			Estimate: func(s string) int {
				cancel()
				return len(s)
			}})
	if res != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result = %+v, error = %v", res, err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("sent %d requests after cancellation", n)
	}
}
