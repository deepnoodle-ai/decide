package compact_test

import (
	"errors"
	"math"
	"testing"

	"github.com/deepnoodle-ai/decide/x/compact"
)

type want struct {
	action compact.Action
	cause  compact.Cause
}

func check(t *testing.T, got []compact.Decision, wants []want) {
	t.Helper()
	if len(got) != len(wants) {
		t.Fatalf("%d decisions, want %d", len(got), len(wants))
	}
	for i, w := range wants {
		if got[i].Index != i || got[i].Action != w.action || got[i].Cause != w.cause {
			t.Errorf("segment %d = %s/%s, want %s/%s", i, got[i].Action, got[i].Cause, w.action, w.cause)
		}
	}
}

func seg(size int) compact.Segment { return compact.Segment{Text: "t", Size: size} }

func needed(p float64) compact.Scores { return compact.Scores{Needed: p} }

func TestSelectOrderAndSkip(t *testing.T) {
	segs := []compact.Segment{
		{Text: "system", Size: 2, Pinned: true},
		seg(3), // 0.9: kept
		seg(5), // 0.8: does not fit after the 0.9, dropped
		seg(1), // 0.5: fits after the skip
		seg(1), // 0.1: below floor
	}
	scores := []compact.Scores{{}, needed(0.9), needed(0.8), needed(0.5), needed(0.1)}
	got, err := compact.Select(segs, scores, compact.Rule{Budget: 7, Floor: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []want{
		{compact.Keep, compact.CausePinned},
		{compact.Keep, compact.CauseRanked},
		{compact.Drop, compact.CauseBudget},
		{compact.Keep, compact.CauseRanked},
		{compact.Drop, compact.CauseFloor},
	})
}

func TestSelectTiesPreferRecent(t *testing.T) {
	segs := []compact.Segment{seg(1), seg(1), seg(1)}
	scores := []compact.Scores{needed(0.5), needed(0.5), needed(0.5)}
	got, err := compact.Select(segs, scores, compact.Rule{Budget: 2})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []want{
		{compact.Drop, compact.CauseBudget},
		{compact.Keep, compact.CauseRanked},
		{compact.Keep, compact.CauseRanked},
	})
}

func TestSelectShortForms(t *testing.T) {
	withShort := compact.Segment{Text: "long", Short: "s", Size: 10, ShortSize: 2}
	segs := []compact.Segment{withShort, withShort, withShort, withShort}
	scores := []compact.Scores{
		{Needed: 0.9, Verbatim: 0.1, HasVerbatim: true}, // short by cutoff
		{Needed: 0.8, Verbatim: 0.9, HasVerbatim: true}, // full fits
		{Needed: 0.7}, // full does not fit, short does
		{Needed: 0.6}, // nothing fits
	}
	got, err := compact.Select(segs, scores, compact.Rule{Budget: 14, ShortBelow: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []want{
		{compact.Short, compact.CauseRanked},
		{compact.Keep, compact.CauseRanked},
		{compact.Short, compact.CauseRanked},
		{compact.Drop, compact.CauseBudget},
	})
}

func TestSelectUnscoredFirst(t *testing.T) {
	boom := errors.New("boom")
	segs := []compact.Segment{seg(2), seg(2), seg(2)}
	scores := []compact.Scores{needed(1), {Err: boom}, {Err: boom}}
	got, err := compact.Select(segs, scores, compact.Rule{Budget: 4, Floor: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []want{
		{compact.Drop, compact.CauseBudget},
		{compact.Keep, compact.CauseUnscored},
		{compact.Keep, compact.CauseUnscored},
	})
	if !errors.Is(got[1].Scores.Err, boom) {
		t.Error("unscored error not kept")
	}
}

func TestSelectPinnedOverBudget(t *testing.T) {
	segs := []compact.Segment{{Size: 5, Pinned: true}, seg(1)}
	got, err := compact.Select(segs, []compact.Scores{{}, needed(1)}, compact.Rule{Budget: 4})
	if !errors.Is(err, compact.ErrOverBudget) {
		t.Fatalf("err = %v", err)
	}
	check(t, got, []want{
		{compact.Keep, compact.CausePinned},
		{compact.Drop, compact.CauseBudget},
	})
}

func TestSelectInvalid(t *testing.T) {
	ok := compact.Rule{Budget: 1}
	cases := map[string]struct {
		segs   []compact.Segment
		scores []compact.Scores
		rule   compact.Rule
	}{
		"lengths":   {[]compact.Segment{seg(1)}, nil, ok},
		"budget":    {nil, nil, compact.Rule{}},
		"floor":     {nil, nil, compact.Rule{Budget: 1, Floor: 2}},
		"cutoff":    {nil, nil, compact.Rule{Budget: 1, ShortBelow: math.NaN()}},
		"size":      {[]compact.Segment{seg(-1)}, []compact.Scores{needed(1)}, ok},
		"needed":    {[]compact.Segment{seg(1)}, []compact.Scores{needed(1.5)}, ok},
		"verbatim":  {[]compact.Segment{seg(1)}, []compact.Scores{{Needed: 1, Verbatim: -1, HasVerbatim: true}}, ok},
		"nan score": {[]compact.Segment{seg(1)}, []compact.Scores{needed(math.NaN())}, ok},
	}
	for name, c := range cases {
		if _, err := compact.Select(c.segs, c.scores, c.rule); !errors.Is(err, compact.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A pinned or unscored segment's numbers are not checked.
	segs := []compact.Segment{{Size: 1, Pinned: true}, seg(0)}
	scores := []compact.Scores{needed(9), {Needed: 9, Err: errors.New("x")}}
	if _, err := compact.Select(segs, scores, ok); err != nil {
		t.Errorf("err = %v", err)
	}
}
