package compact_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/patterns/compact"
)

var transcript = []compact.Segment{
	{Text: "You are a coding agent.", Size: 5, Pinned: true},
	{Text: "read_file main.go -> 400 lines", Short: "read main.go", Size: 40, ShortSize: 4},
	{Text: "run tests -> 2 failures in parser_test.go", Size: 10},
	{Text: "ls -> README.md go.mod", Size: 6},
}

// answer returns a responder that answers "<i>.needed" and "<i>.verbatim"
// from the maps, and 0.5 otherwise.
func answer(needed, verbatim map[int]float64) decidetest.Responder {
	return func(req *decide.Request) (*decide.Response, error) {
		resp := &decide.Response{Answers: map[string]decide.Answer{},
			Usage: decide.Usage{InputTokens: 100, OutputTokens: len(req.Questions)}}
		for key := range req.Questions {
			p := 0.5
			var i int
			var kind string
			if n, _ := strings.CutSuffix(key, ".needed"); n != key {
				kind = "needed"
				i = atoi(n)
			} else if n, _ := strings.CutSuffix(key, ".verbatim"); n != key {
				kind = "verbatim"
				i = atoi(n)
			}
			m := needed
			if kind == "verbatim" {
				m = verbatim
			}
			if v, ok := m[i]; ok {
				p = v
			}
			resp.Answers[key] = decidetest.NoulAnswer(p)
		}
		return resp, nil
	}
}

// instructions decodes a recorded Noul's JSON instructions.
func instructions(t *testing.T, q decide.Question) map[string]any {
	t.Helper()
	b, err := json.Marshal(q.(*decide.NoulQuestion).Instructions)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestCompactEndToEnd(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Respond(answer(map[int]float64{1: 0.9, 2: 0.95, 3: 0.1}, map[int]float64{1: 0.2}))
	res, err := compact.Compact(context.Background(), srv.Client(t), transcript, compact.Config{
		Focus:   "Fix the failing parser test.",
		Rule:    compact.Rule{Budget: 30, Floor: 0.3, ShortBelow: 0.5},
		Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	check(t, res.Decisions, []want{
		{compact.Keep, compact.CausePinned},
		{compact.Short, compact.CauseRanked},
		{compact.Keep, compact.CauseRanked},
		{compact.Drop, compact.CauseFloor},
	})
	if got := res.Apply(transcript); !slices.Equal(got, []string{
		"You are a coding agent.", "read main.go", "run tests -> 2 failures in parser_test.go",
	}) {
		t.Errorf("Apply = %q", got)
	}
	if res.Before != 61 || res.After != 19 || res.Requests != 1 || res.InputTokens != 100 ||
		res.OutputTokens != 4 || len(res.Models) != 1 {
		t.Errorf("report = %+v", res)
	}

	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	req := reqs[0].Request
	if keys := slices.Sorted(maps.Keys(req.Questions)); !slices.Equal(keys,
		[]string{"1.needed", "1.verbatim", "2.needed", "3.needed"}) {
		t.Errorf("keys = %v", keys)
	}
	if req.State != "Fix the failing parser test." {
		t.Errorf("state = %v", req.State)
	}
	in := instructions(t, req.Questions["1.verbatim"])
	if in["question"] != compact.DefaultVerbatim || in["segment"] != transcript[1].Text ||
		in["short_form"] != transcript[1].Short {
		t.Errorf("verbatim instructions = %v", in)
	}
}

func TestCompactNoVerbatimWhenDisabled(t *testing.T) {
	srv := decidetest.NewServer(t)
	_, err := compact.Compact(context.Background(), srv.Client(t), transcript, compact.Config{
		Focus: "f", Rule: compact.Rule{Budget: 100}, Workers: 1, Needed: "Still needed?",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := srv.Requests()[0].Request
	if _, ok := req.Questions["1.verbatim"]; ok {
		t.Error("verbatim asked with ShortBelow = 0")
	}
	in := instructions(t, req.Questions["2.needed"])
	if in["question"] != "Still needed?" {
		t.Errorf("wording = %v", in["question"])
	}
}

func TestCompactPacking(t *testing.T) {
	srv := decidetest.NewServer(t)
	segs := []compact.Segment{
		{Text: "a", Size: 1}, {Text: "b", Size: 1}, {Text: "c", Size: 1},
		{Text: "huge", Size: 1},
	}
	// Each question estimates to 10, request framing and state to 5;
	// anything containing "huge" estimates to 100.
	estimate := func(s string) int {
		switch {
		case s == `"f"`:
			return 5
		case strings.Contains(s, "huge"):
			return 100
		}
		var req struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if json.Unmarshal([]byte(s), &req) == nil && req.Questions != nil {
			return 5 + 10*len(req.Questions)
		}
		return 10
	}
	res, err := compact.Compact(context.Background(), srv.Client(t), segs, compact.Config{
		Focus: "f", Rule: compact.Rule{Budget: 10}, Workers: 1, Estimate: estimate,
		Limits: compact.Limits{Request: 25, StateAndQuestion: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 || len(reqs[0].Request.Questions) != 2 || len(reqs[1].Request.Questions) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	d := res.Decisions[3]
	if !errors.Is(d.Scores.Err, compact.ErrTooLarge) || d.Cause != compact.CauseUnscored {
		t.Errorf("huge = %+v", d)
	}

	// The state alone over StateAndQuestion fails the call.
	_, err = compact.Compact(context.Background(), srv.Client(t), segs, compact.Config{
		Focus: "f", Rule: compact.Rule{Budget: 10}, Workers: 1, Estimate: estimate,
		Limits: compact.Limits{Request: 25, StateAndQuestion: 4},
	})
	if !errors.Is(err, compact.ErrTooLarge) {
		t.Errorf("err = %v", err)
	}
}

func TestCompactInvalidAnswerIsUnscored(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("2.needed", decidetest.NoulAnswer(1.5))
	res, err := compact.Compact(context.Background(), srv.Client(t, decide.WithValidation(false)),
		transcript, compact.Config{Focus: "f", Rule: compact.Rule{Budget: 100}, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Decisions[2]
	if !errors.Is(d.Scores.Err, decide.ErrInvalidAnswer) || d.Cause != compact.CauseUnscored {
		t.Errorf("decision = %+v", d)
	}
	if res.Decisions[1].Cause != compact.CauseRanked {
		t.Errorf("other segment = %+v", res.Decisions[1])
	}
}

func TestCompactRequestFailure(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.FailNext(http.StatusUnauthorized)
	res, err := compact.Compact(context.Background(), srv.Client(t), transcript,
		compact.Config{Focus: "f", Rule: compact.Rule{Budget: 100}, Workers: 1})
	if res != nil || !errors.Is(err, decide.ErrAuth) {
		t.Errorf("res = %v, err = %v", res, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err = compact.Compact(ctx, srv.Client(t), transcript,
		compact.Config{Focus: "f", Rule: compact.Rule{Budget: 100}, Workers: 1})
	if res != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("res = %v, err = %v", res, err)
	}
}

func TestCompactOverBudget(t *testing.T) {
	srv := decidetest.NewServer(t)
	res, err := compact.Compact(context.Background(), srv.Client(t), transcript,
		compact.Config{Focus: "f", Rule: compact.Rule{Budget: 1}, Workers: 1})
	if !errors.Is(err, compact.ErrOverBudget) || res == nil || res.After != 5 || res.Requests != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("%d requests sent", n)
	}
	for _, d := range res.Decisions[1:] {
		if d.Action != compact.Drop || d.Cause != compact.CauseBudget ||
			!errors.Is(d.Scores.Err, compact.ErrOverBudget) {
			t.Errorf("decision = %+v", d)
		}
	}
}

func TestCompactClampsBoundaryNoise(t *testing.T) {
	for _, p := range []float64{-5e-7, 1 + 5e-7} {
		srv := decidetest.NewServer(t)
		srv.Respond(answer(map[int]float64{1: p, 2: p, 3: p}, map[int]float64{1: p}))
		res, err := compact.Compact(context.Background(), srv.Client(t), transcript, compact.Config{
			Focus: "f", Rule: compact.Rule{Budget: 100, ShortBelow: 0.5}, Workers: 1})
		if err != nil {
			t.Fatalf("p = %v: %v", p, err)
		}
		want := min(max(p, 0), 1)
		for _, d := range res.Decisions[1:] {
			if d.Cause != compact.CauseRanked || d.Scores.Needed != want {
				t.Errorf("p = %v: decision = %+v", p, d)
			}
		}
		if s := res.Decisions[1].Scores; !s.HasVerbatim || s.Verbatim != want {
			t.Errorf("p = %v: verbatim = %+v", p, s)
		}
	}
}

func TestCompactInvalidConfig(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	good := compact.Config{Focus: "f", Rule: compact.Rule{Budget: 1}, Workers: 1}
	cases := map[string]func(*compact.Config) (context.Context, *decide.Client){
		"nil ctx":    func(*compact.Config) (context.Context, *decide.Client) { return nil, client },
		"nil client": func(*compact.Config) (context.Context, *decide.Client) { return context.Background(), nil },
		"workers": func(c *compact.Config) (context.Context, *decide.Client) {
			c.Workers = 0
			return context.Background(), client
		},
		"limits": func(c *compact.Config) (context.Context, *decide.Client) {
			c.Limits = compact.Limits{Request: 1}
			return context.Background(), client
		},
		"rule": func(c *compact.Config) (context.Context, *decide.Client) {
			c.Rule.Budget = 0
			return context.Background(), client
		},
		"focus": func(c *compact.Config) (context.Context, *decide.Client) {
			c.Focus = func() {}
			return context.Background(), client
		},
	}
	for name, mutate := range cases {
		cfg := good
		ctx, cl := mutate(&cfg)
		if _, err := compact.Compact(ctx, cl, transcript, cfg); !errors.Is(err, compact.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := compact.Compact(context.Background(), client,
		[]compact.Segment{seg(-1)}, good); !errors.Is(err, compact.ErrInvalidInput) {
		t.Errorf("negative size: err = %v", err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("%d requests sent", n)
	}
}
