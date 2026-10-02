package funnel_test

import (
	"context"
	"slices"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/x/funnel"
)

func emptyOrAsked() funnel.Layout[string] {
	return funnel.Shared("state", func(it funnel.Item[string]) (map[string]decide.Question, error) {
		if it.Value != "asked" {
			return nil, nil
		}
		return map[string]decide.Question{"q": decide.Noul("Needed?")}, nil
	}, 0)
}

func TestSharedEmptyQuestionsRespectKeep(t *testing.T) {
	srv := decidetest.NewServer(t)
	var seen []int
	res, err := funnel.Run(context.Background(), srv.Client(t), []string{"empty", "asked"}, 1,
		funnel.Stage[string]{Name: "screen", Ask: emptyOrAsked(), Keep: func(it funnel.Item[string]) (bool, error) {
			seen = append(seen, it.Index)
			return it.Value == "asked", nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []int{0, 1}) || !slices.Equal(survivors(res), []string{"asked"}) {
		t.Fatalf("keep saw %v, survivors %v", seen, survivors(res))
	}
	if d := res.Items[0].Drop; d == nil || d.Stage != "screen" || d.Cause != funnel.CauseKeep || d.Err != nil {
		t.Fatalf("empty item drop = %+v", d)
	}
	if r := res.Stages[0]; r.In != 2 || r.Out != 1 || r.Failed != 0 || r.Responses != 1 {
		t.Fatalf("report = %+v", r)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("sent %d requests", n)
	}
}

func TestSharedEmptyQuestionsRespectLimit(t *testing.T) {
	srv := decidetest.NewServer(t)
	var ranked []int
	res, err := funnel.Run(context.Background(), srv.Client(t), []string{"empty", "asked", "empty2"}, 1,
		funnel.Stage[string]{Name: "screen", Ask: emptyOrAsked(), Limit: 1,
			Rank: func(it funnel.Item[string]) (float64, error) {
				ranked = append(ranked, it.Index)
				return float64(it.Index), nil
			}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ranked, []int{0, 1, 2}) || !slices.Equal(survivors(res), []string{"empty2"}) {
		t.Fatalf("rank saw %v, survivors %v", ranked, survivors(res))
	}
	for _, i := range []int{0, 1} {
		if d := res.Items[i].Drop; d == nil || d.Stage != "screen" || d.Cause != funnel.CauseLimit || d.Err != nil {
			t.Fatalf("item %d drop = %+v", i, d)
		}
	}
	if r := res.Stages[0]; r.In != 3 || r.Out != 1 || r.Failed != 0 || r.Responses != 1 {
		t.Fatalf("report = %+v", r)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("sent %d requests", n)
	}
}

func TestSharedEmptyQuestionsHaveStageAnswers(t *testing.T) {
	srv := decidetest.NewServer(t)
	res, err := funnel.Run(context.Background(), srv.Client(t), []string{"empty"}, 1,
		funnel.Stage[string]{Name: "screen", Ask: emptyOrAsked(), Keep: func(it funnel.Item[string]) (bool, error) {
			answers, ok := it.Answers["screen"]
			if !ok || answers == nil || len(answers) != 0 {
				t.Errorf("empty stage answers = %v, present = %v", answers, ok)
			}
			return true, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.Stages[0]; r.Out != 1 || r.Responses != 0 {
		t.Fatalf("report = %+v", r)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("sent %d requests for an empty stage", n)
	}
}
