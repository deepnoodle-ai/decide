package cli

import (
	"testing"

	"github.com/deepnoodle-ai/decide/internal/skill"
)

func TestJudge(t *testing.T) {
	yes := []skill.Condition{{Answer: "yes", Op: ">=", Value: 0.6}}
	strict := []skill.Condition{{Answer: "yes", Op: ">=", Value: 0.8}}
	negative := []skill.Condition{{Answer: "negative", Op: ">=", Value: 0.6}}
	low := []skill.Condition{{Op: "<=", Value: 1.5}}
	noul := func(p float64) answer { return answer{Type: "noul", Noul: p} }
	choice := func(neg float64) answer {
		return answer{Type: "choice", Probabilities: map[string]float64{"negative": neg, "positive": 1 - neg}}
	}
	score := func(s float64) answer { return answer{Type: "score", Score: s} }
	for _, tc := range []struct {
		name  string
		conds []skill.Condition
		a     answer
		want  verdict
		match []skill.Condition
	}{
		{"no flag, sure", nil, noul(0.9), plain, nil},
		{"no flag, unsure", nil, noul(0.5), near, nil},
		{"no flag, at 40%", nil, noul(0.4), plain, nil},
		{"flagged at 60%", yes, noul(0.6), flagged, nil},
		{"unsure below 60%", yes, noul(0.59), near, nil},
		{"clear at 40%", yes, noul(0.4), clear, nil},
		{"clear when no", yes, noul(0.1), clear, nil},
		{"custom threshold not met", strict, noul(0.7), near, nil},
		{"custom threshold met", strict, noul(0.8), flagged, nil},
		{"flag on no", []skill.Condition{{Answer: "no", Op: ">=", Value: 0.6}}, noul(0.2), flagged, nil},
		{"choice flagged", negative, choice(0.7), flagged, nil},
		{"choice close", negative, choice(0.45), near, nil},
		{"choice clear", negative, choice(0.1), clear, nil},
		{"score flagged", low, score(1.5), flagged, nil},
		{"score clear", low, score(1.6), clear, nil},
		{"matched", nil, noul(0.7), matched, yes},
		{"possible match", nil, noul(0.45), near, yes},
		{"no match", nil, noul(0.2), unmatched, yes},
		{"flag outweighs match", yes, noul(0.9), flagged, yes},
		{"clear with a match elsewhere", low, score(3), clear, []skill.Condition{{Op: ">=", Value: 3.5}}},
	} {
		if got := judge(tc.conds, tc.match, tc.a); got != tc.want {
			t.Errorf("%s: judge = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTrack(t *testing.T) {
	for _, tc := range []struct {
		score float64
		top   int
		want  string
	}{
		{0, 4, "────────────"},
		{0.9, 4, "━━╸─────────"},
		{2.5, 4, "━━━━━━━╸────"},
		{4, 4, "━━━━━━━━━━━━"},
		{1, 0, "────────────"},
	} {
		if got := stripANSI(track(tc.score, tc.top, plain)); got != tc.want {
			t.Errorf("track(%g, %d) = %q, want %q", tc.score, tc.top, got, tc.want)
		}
	}
}
