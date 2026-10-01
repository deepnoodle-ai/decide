package funnel_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/funnel"
)

var tools = []string{"calendar", "email", "search", "weather"}

// screen asks one Noul per item against a shared state.
func screen(size int) funnel.Layout[string] {
	return funnel.Shared("Book a meeting with Sam on Friday.",
		func(it funnel.Item[string]) (map[string]decide.Question, error) {
			return map[string]decide.Question{
				"fits": decide.Noul("Does the " + it.Value + " tool fit the request?"),
			}, nil
		}, size)
}

func noul(t *testing.T, it funnel.Item[string], stage, local string) float64 {
	t.Helper()
	a, err := funnel.AnswerAs[*decide.NoulAnswer](it, stage, local)
	if err != nil {
		t.Fatal(err)
	}
	return a.Noul
}

// nouls answers every Noul with the value for the tool named in its
// instructions or state.
func nouls(values map[string]float64) decidetest.Responder {
	return func(req *decide.Request) (*decide.Response, error) {
		resp := &decide.Response{Answers: map[string]decide.Answer{},
			Usage: decide.Usage{InputTokens: 10, OutputTokens: len(req.Questions)}}
		for key, q := range req.Questions {
			text := fmt.Sprint(q.(*decide.NoulQuestion).Instructions, req.State)
			for name, p := range values {
				if strings.Contains(text, name) {
					resp.Answers[key] = decidetest.NoulAnswer(p)
				}
			}
		}
		return resp, nil
	}
}

func survivors(r *funnel.Result[string]) []string {
	var out []string
	for _, it := range r.Survivors() {
		out = append(out, it.Value)
	}
	return out
}

func TestTwoStagesCarryAnswers(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Respond(nouls(map[string]float64{"calendar": 0.9, "email": 0.6, "search": 0.6, "weather": 0.1}))
	var mu sync.Mutex
	var seen []float64
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 2,
		funnel.Stage[string]{
			Name: "screen",
			Ask:  screen(0),
			Keep: func(it funnel.Item[string]) (bool, error) {
				return noul(t, it, "screen", "fits") >= 0.5, nil
			},
			Rank: func(it funnel.Item[string]) (float64, error) {
				return noul(t, it, "screen", "fits"), nil
			},
			Limit: 2,
		},
		funnel.Stage[string]{
			Name: "check",
			Ask: funnel.PerItem(func(_ context.Context, it funnel.Item[string]) (*decide.Request, error) {
				mu.Lock()
				seen = append(seen, noul(t, it, "screen", "fits"))
				mu.Unlock()
				req := decide.NewRequest("full description of " + it.Value)
				req.Questions["works"] = decide.Noul("Can this tool do it alone?")
				return req, nil
			}),
		})
	if err != nil {
		t.Fatal(err)
	}
	// email and search tie at 0.6; input order keeps email.
	if got := survivors(res); !slices.Equal(got, []string{"calendar", "email"}) {
		t.Fatalf("survivors = %v", got)
	}
	if !slices.Equal(seen, []float64{0.9, 0.6}) && !slices.Equal(seen, []float64{0.6, 0.9}) {
		t.Errorf("stage two saw %v", seen)
	}
	want := map[string]funnel.Drop{
		"search":  {Stage: "screen", Cause: funnel.CauseLimit},
		"weather": {Stage: "screen", Cause: funnel.CauseKeep},
	}
	for _, it := range res.Items {
		if w, ok := want[it.Value]; ok && (it.Drop == nil || *it.Drop != w) {
			t.Errorf("%s drop = %+v, want %+v", it.Value, it.Drop, w)
		}
	}
	if got := noul(t, res.Items[0], "check", "works"); got != 0.9 {
		t.Errorf("check answer = %v", got)
	}

	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 1 screen + 2 checks", len(reqs))
	}
	if keys := slices.Sorted(maps.Keys(reqs[0].Request.Questions)); !slices.Equal(keys,
		[]string{"0.fits", "1.fits", "2.fits", "3.fits"}) {
		t.Errorf("screen keys = %v", keys)
	}
	r0, r1 := res.Stages[0], res.Stages[1]
	if r0.In != 4 || r0.Out != 2 || r0.Responses != 1 || r0.InputTokens != 10 || r0.OutputTokens != 4 {
		t.Errorf("screen report = %+v", r0)
	}
	if r1.In != 2 || r1.Out != 2 || r1.Responses != 2 || r1.InputTokens != 20 || r1.OutputTokens != 2 {
		t.Errorf("check report = %+v", r1)
	}
	if len(r0.Models) != 1 {
		t.Errorf("models = %v", r0.Models)
	}
}

func TestSharedChunks(t *testing.T) {
	srv := decidetest.NewServer(t)
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "screen", Ask: screen(3)})
	if err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 || len(reqs[0].Request.Questions) != 3 || len(reqs[1].Request.Questions) != 1 {
		t.Fatalf("chunks = %d requests", len(reqs))
	}
	if res.Stages[0].Responses != 2 || res.Stages[0].Out != 4 {
		t.Errorf("report = %+v", res.Stages[0])
	}
}

func TestSharedItemErrorsStayLocal(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Respond(func(req *decide.Request) (*decide.Response, error) {
		resp := &decide.Response{Answers: map[string]decide.Answer{}}
		for key := range req.Questions {
			switch key {
			case "1.fits": // out of range
				resp.Answers[key] = decidetest.NoulAnswer(1.5)
			case "2.fits": // missing
			default:
				resp.Answers[key] = decidetest.NoulAnswer(0.5)
			}
		}
		resp.Answers["stray"] = decidetest.NoulAnswer(0.5)
		return resp, nil
	})
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "screen", Ask: screen(0)})
	if err != nil {
		t.Fatal(err)
	}
	if got := survivors(res); !slices.Equal(got, []string{"calendar", "weather"}) {
		t.Fatalf("survivors = %v", got)
	}
	for _, i := range []int{1, 2} {
		d := res.Items[i].Drop
		if d == nil || d.Cause != funnel.CauseError || !errors.Is(d.Err, decide.ErrInvalidAnswer) {
			t.Errorf("item %d drop = %+v", i, d)
		}
		if _, ok := errors.AsType[*decide.AnswerError](d.Err); !ok {
			t.Errorf("item %d: no AnswerError in %v", i, d.Err)
		}
	}
	if r := res.Stages[0]; r.Failed != 2 || r.Out != 2 || r.Responses != 1 {
		t.Errorf("report = %+v", r)
	}
}

func TestValidationWithRootValidationOff(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.Answer("0.fits", decidetest.NoulAnswer(-1))
	client := srv.Client(t, decide.WithValidation(false))
	res, err := funnel.Run(context.Background(), client, tools[:2], 1,
		funnel.Stage[string]{Name: "screen", Ask: screen(0)})
	if err != nil {
		t.Fatal(err)
	}
	if d := res.Items[0].Drop; d == nil || !errors.Is(d.Err, decide.ErrInvalidAnswer) {
		t.Errorf("drop = %+v", d)
	}
	if res.Items[1].Drop != nil {
		t.Errorf("item 1 dropped: %+v", res.Items[1].Drop)
	}
}

func TestRequestAndBuildErrors(t *testing.T) {
	srv := decidetest.NewServer(t)
	srv.FailNext(http.StatusUnauthorized)
	boom := errors.New("boom")
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "check", Ask: funnel.PerItem(
			func(_ context.Context, it funnel.Item[string]) (*decide.Request, error) {
				switch it.Value {
				case "search":
					return nil, boom
				case "weather":
					return nil, nil
				}
				req := decide.NewRequest(it.Value)
				req.Questions["q"] = decide.Noul("?")
				return req, nil
			})})
	if err != nil {
		t.Fatal(err)
	}
	// One worker: calendar gets the 401, email succeeds.
	if got := survivors(res); !slices.Equal(got, []string{"email"}) {
		t.Fatalf("survivors = %v", got)
	}
	checks := map[int]error{0: decide.ErrAuth, 2: boom, 3: decide.ErrInvalidRequest}
	for i, want := range checks {
		if d := res.Items[i].Drop; d == nil || !errors.Is(d.Err, want) {
			t.Errorf("item %d drop = %+v, want %v", i, d, want)
		}
	}
	if r := res.Stages[0]; r.Failed != 3 || r.Responses != 1 {
		t.Errorf("report = %+v", r)
	}
}

func TestQuestionErrorsAndEmpty(t *testing.T) {
	srv := decidetest.NewServer(t)
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "screen", Ask: funnel.Shared("s",
			func(it funnel.Item[string]) (map[string]decide.Question, error) {
				switch it.Value {
				case "calendar":
					return nil, errors.New("no")
				case "email":
					return map[string]decide.Question{"q": (*decide.NoulQuestion)(nil)}, nil
				case "search":
					return nil, nil // nothing to ask; goes to Keep
				}
				return map[string]decide.Question{"q": decide.Noul("?")}, nil
			}, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if got := survivors(res); !slices.Equal(got, []string{"search", "weather"}) {
		t.Fatalf("survivors = %v", got)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestEmptyStageSendsNothing(t *testing.T) {
	srv := decidetest.NewServer(t)
	never := func(funnel.Item[string]) (bool, error) { return false, nil }
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 2,
		funnel.Stage[string]{Name: "a", Ask: screen(0), Keep: never},
		funnel.Stage[string]{Name: "b", Ask: screen(0)})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
	if r := res.Stages[1]; r.In != 0 || r.Responses != 0 {
		t.Errorf("report = %+v", r)
	}

	res, err = funnel.Run(context.Background(), srv.Client(t), []string{}, 2,
		funnel.Stage[string]{Name: "a", Ask: screen(0)})
	if err != nil || len(res.Stages) != 1 || !reflect.DeepEqual(res.Stages[0], funnel.Report{Name: "a"}) {
		t.Errorf("empty run = %+v, %v", res, err)
	}
}

func TestKeepAndRankErrors(t *testing.T) {
	srv := decidetest.NewServer(t)
	res, err := funnel.Run(context.Background(), srv.Client(t), tools, 1,
		funnel.Stage[string]{
			Name: "screen",
			Ask:  screen(0),
			Keep: func(it funnel.Item[string]) (bool, error) {
				if it.Value == "calendar" {
					return false, errors.New("keep failed")
				}
				return true, nil
			},
			Rank: func(it funnel.Item[string]) (float64, error) {
				if it.Value == "email" {
					return 0, errors.New("rank failed")
				}
				return 0, nil
			},
			Limit: 1,
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := survivors(res); !slices.Equal(got, []string{"search"}) {
		t.Fatalf("survivors = %v", got)
	}
	causes := []funnel.Cause{funnel.CauseError, funnel.CauseError, "", funnel.CauseLimit}
	for i, c := range causes {
		if d := res.Items[i].Drop; (d == nil) != (c == "") || (d != nil && d.Cause != c) {
			t.Errorf("item %d drop = %+v, want %q", i, d, c)
		}
	}
}

func TestCancellationStopsLaterStages(t *testing.T) {
	srv := decidetest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	res, err := funnel.Run(ctx, srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "a", Ask: screen(0),
			Keep: func(funnel.Item[string]) (bool, error) { cancel(); return true, nil }},
		funnel.Stage[string]{Name: "b", Ask: screen(0)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(res.Survivors()) != 0 {
		t.Errorf("survivors after cancel: %v", survivors(res))
	}
	for _, it := range res.Items {
		if it.Drop.Stage != "b" || !errors.Is(it.Drop.Err, context.Canceled) {
			t.Errorf("drop = %+v", it.Drop)
		}
		if _, ok := it.Answers["a"]["fits"]; !ok {
			t.Errorf("item %d lost stage a answer", it.Index)
		}
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests", n)
	}
}

func TestCancellationInLastStage(t *testing.T) {
	srv := decidetest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	res, err := funnel.Run(ctx, srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "a", Ask: screen(0),
			Keep: func(funnel.Item[string]) (bool, error) { cancel(); return true, nil }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	for _, it := range res.Items {
		if _, ok := it.Answers["a"]["fits"]; !ok {
			t.Errorf("item %d lost stage a answer", it.Index)
		}
	}
}

func TestCanceledBeforeRun(t *testing.T) {
	srv := decidetest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := funnel.Run(ctx, srv.Client(t), tools, 1,
		funnel.Stage[string]{Name: "a", Ask: screen(0)})
	if !errors.Is(err, context.Canceled) || res.Stages[0].Failed != 4 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestInvalidConfig(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	rank := func(funnel.Item[string]) (float64, error) { return 0, nil }
	ok := funnel.Stage[string]{Name: "a", Ask: screen(0)}
	cases := map[string]struct {
		ctx     context.Context
		client  *decide.Client
		workers int
		stages  []funnel.Stage[string]
	}{
		"nil ctx":     {nil, client, 1, []funnel.Stage[string]{ok}},
		"nil client":  {context.Background(), nil, 1, []funnel.Stage[string]{ok}},
		"workers":     {context.Background(), client, 0, []funnel.Stage[string]{ok}},
		"no stages":   {context.Background(), client, 1, nil},
		"empty name":  {context.Background(), client, 1, []funnel.Stage[string]{{Ask: screen(0)}}},
		"repeat name": {context.Background(), client, 1, []funnel.Stage[string]{ok, ok}},
		"zero layout": {context.Background(), client, 1, []funnel.Stage[string]{{Name: "a"}}},
		"nil build":   {context.Background(), client, 1, []funnel.Stage[string]{{Name: "a", Ask: funnel.PerItem[string](nil)}}},
		"nil shared":  {context.Background(), client, 1, []funnel.Stage[string]{{Name: "a", Ask: funnel.Shared[string]("s", nil, 0)}}},
		"neg size":    {context.Background(), client, 1, []funnel.Stage[string]{{Name: "a", Ask: screen(-1)}}},
		"neg limit":   {context.Background(), client, 1, []funnel.Stage[string]{{Name: "a", Ask: screen(0), Rank: rank, Limit: -1}}},
		"limit, rank": {context.Background(), client, 1, []funnel.Stage[string]{{Name: "a", Ask: screen(0), Limit: 1}}},
	}
	for name, c := range cases {
		res, err := funnel.Run(c.ctx, c.client, tools, c.workers, c.stages...)
		if res != nil || !errors.Is(err, funnel.ErrInvalidConfig) {
			t.Errorf("%s: res = %v, err = %v", name, res, err)
		}
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("%d requests sent", n)
	}
}

func TestAnswerAs(t *testing.T) {
	it := funnel.Item[string]{Answers: map[string]map[string]decide.Answer{
		"a": {"q": decidetest.NoulAnswer(0.3)},
	}}
	if _, err := funnel.AnswerAs[*decide.ScoreAnswer](it, "a", "q"); err == nil {
		t.Error("wrong type accepted")
	}
	if _, err := funnel.AnswerAs[*decide.NoulAnswer](it, "b", "q"); err == nil {
		t.Error("missing stage accepted")
	}
	if a, err := funnel.AnswerAs[*decide.NoulAnswer](it, "a", "q"); err != nil || a.Noul != 0.3 {
		t.Errorf("a = %v, err = %v", a, err)
	}
}
