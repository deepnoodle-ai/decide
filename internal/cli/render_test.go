package cli

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/skill"
	"github.com/deepnoodle-ai/wonton/color"
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

func TestCombineParts(t *testing.T) {
	part := func(n int, lines string, answers map[string]answer) runs.Result {
		raw := map[string]json.RawMessage{}
		for k, a := range answers {
			raw[k], _ = json.Marshal(a)
		}
		return runs.Result{Index: n - 1, Source: "big.md", Status: "complete", Answers: raw,
			Part: &runs.Part{N: n, Of: 2, Lines: lines}}
	}
	mood := func(pos float64) answer {
		return answer{Type: "choice", Probabilities: map[string]float64{"positive": pos, "negative": 1 - pos}}
	}
	score := func(probs ...float64) answer {
		a := answer{Type: "score", Probabilities: map[string]float64{}}
		for i, p := range probs {
			a.Probabilities[strconv.Itoa(i)] = p
			a.Score += float64(i) * p
		}
		return a
	}
	parts := []runs.Result{
		part(1, "1-30", map[string]answer{"mood": mood(0.9), "clarity": score(0, 0, 1), "low": score(0, 0, 1)}),
		part(2, "31-40", map[string]answer{"mood": mood(0.1), "clarity": score(1, 0, 0), "low": score(1, 0, 0)}),
	}
	m := marks{
		flags:   map[string][]skill.Condition{"low": {{Op: "<=", Value: 0.5}}},
		matches: map[string][]skill.Condition{},
	}
	it := combine(parts, m)
	get := func(key string) answer {
		var a answer
		json.Unmarshal(it.Answers[key], &a)
		return a
	}
	// Unmarked answers are averaged by lines: 30 lines at 0.9, 10 at 0.1.
	if a := get("mood"); a.Choice != "positive" || math.Abs(a.Probabilities["positive"]-0.7) > 1e-9 {
		t.Fatalf("mood = %+v", a)
	}
	if a := get("clarity"); math.Abs(a.Score-1.5) > 1e-9 {
		t.Fatalf("clarity = %+v", a)
	}
	// A flagged question takes the part closest to its flag.
	if a := get("low"); a.Score != 0 || it.where["low"] != "lines 31-40" || it.where["mood"] != "" {
		t.Fatalf("low = %+v, where = %v", a, it.where)
	}
	if it.parts != 2 || it.Source != "big.md" || it.Part != nil {
		t.Fatalf("item = %+v", it)
	}
}

func TestCollectorShowsFailedItemsWhenStopped(t *testing.T) {
	var got []item
	c := newCollector(marks{}, nil, func(it item) error {
		got = append(got, it)
		return nil
	})
	c.add(runs.Result{Index: 0, Source: "a.md", Status: "failed", Error: "HTTP 413",
		Part: &runs.Part{N: 1, Of: 3, Lines: "1-9"}})
	c.add(runs.Result{Index: 3, Source: "b.md", Status: "complete",
		Part: &runs.Part{N: 1, Of: 2, Lines: "1-5"}})
	if len(got) != 0 {
		t.Fatalf("emitted before the items were whole: %+v", got)
	}
	c.finish()
	if len(got) != 1 || got[0].Source != "a.md" || got[0].Status != "failed" || got[0].Error != "part 1 of 3 (lines 1-9): HTTP 413" {
		t.Fatalf("finish emitted %+v", got)
	}
}

func TestPrinterRemovesControlCharacters(t *testing.T) {
	defer func(on bool) { color.Enabled = on }(color.Enabled)
	color.Enabled = false
	const esc = "\x1b[31m"
	s := &skill.Skill{Questions: skill.Questions{
		{Key: "tone" + esc, Raw: json.RawMessage(`{"type":"choice","criteria":["calm","angry"]}`)},
		{Key: "odd", Raw: json.RawMessage(`{"type":"noul"}`)},
	}}
	tone, _ := json.Marshal(answer{Type: "choice", Choice: "calm" + esc,
		Probabilities: map[string]float64{"calm" + esc: 0.7, "angry": 0.3}})
	odd, _ := json.Marshal(answer{Type: "weird" + esc})
	it := item{Result: runs.Result{Source: "a" + esc + ".md", Status: "complete",
		Input:   json.RawMessage(`"hi` + `\u001b[2J"`),
		Answers: map[string]json.RawMessage{"tone" + esc: tone, "odd": odd}},
		where: map[string]string{"tone" + esc: "lines " + esc}}
	var b strings.Builder
	if err := newPrinter(&b, s, true).item(it); err != nil {
		t.Fatal(err)
	}
	if out := b.String(); strings.ContainsRune(out, 0x1b) {
		t.Fatalf("output contains an escape sequence:\n%q", out)
	}
}
