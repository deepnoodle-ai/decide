package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/skill"
)

// item is one item's outcome: its result, or the results for the parts of
// an item too large to judge whole, combined into one.
type item struct {
	runs.Result                   // the combined result, without a Part
	parts       int               // how many parts it was judged in; 0 when whole
	where       map[string]string // for each question, the lines of the part that decided it
}

// group collects results into items. The parts of an item are consecutive
// and are combined once all of them are present; an item still missing a
// part is left out.
func group(results []runs.Result, m marks) []item {
	var out []item
	for i := 0; i < len(results); i++ {
		res := results[i]
		if res.Part == nil {
			out = append(out, item{Result: res})
			continue
		}
		first := res.Index - (res.Part.N - 1)
		parts := []runs.Result{}
		for ; i < len(results) && results[i].Part != nil && results[i].Index-(results[i].Part.N-1) == first; i++ {
			parts = append(parts, results[i])
		}
		i--
		if len(parts) == res.Part.Of {
			out = append(out, combine(parts, m))
		}
	}
	return out
}

// combine makes one result from the results for an item's parts. A
// question with a flag takes the answer of the part closest to being
// flagged, so an item is flagged when any part is; a question with a
// match takes the part closest to matching. Other answers are averaged,
// weighted by the number of lines in each part.
func combine(parts []runs.Result, m marks) item {
	first := parts[0]
	it := item{parts: len(parts), where: map[string]string{}}
	it.Index, it.Source, it.Input, it.Status = first.Index, first.Source, first.Input, "complete"
	it.Model, it.RequestID = first.Model, first.RequestID
	for _, p := range parts {
		if p.Status != "complete" {
			it.Status = "failed"
			it.Error = fmt.Sprintf("part %d of %d (lines %s): %s", p.Part.N, p.Part.Of, p.Part.Lines, friendlyError(p.Error))
			return it
		}
	}
	it.Answers = map[string]json.RawMessage{}
	for key := range first.Answers {
		answers := make([]answer, 0, len(parts))
		weights := make([]float64, 0, len(parts))
		for _, p := range parts {
			var a answer
			if raw, ok := p.Answers[key]; ok && json.Unmarshal(raw, &a) == nil {
				answers = append(answers, a)
				weights = append(weights, float64(lineCount(p.Part.Lines)))
			}
		}
		if len(answers) < len(parts) {
			continue // a part has no answer to this question
		}
		var a answer
		conds := m.flags[key]
		if len(conds) == 0 {
			conds = m.matches[key]
		}
		if len(conds) > 0 {
			best := 0
			for i := range answers {
				if strength(conds, answers[i]) > strength(conds, answers[best]) {
					best = i
				}
			}
			a = answers[best]
			it.where[key] = parts[best].Part.Lines
		} else {
			a = average(answers, weights)
		}
		if raw, err := json.Marshal(a); err == nil {
			it.Answers[key] = raw
		}
	}
	return it
}

// strength is how close an answer comes to meeting any of the conditions:
// the probability of the answer named, or how far the score lies in the
// direction of the comparison.
func strength(conds []skill.Condition, a answer) float64 {
	prob := probOf(a)
	s := math.Inf(-1)
	for _, c := range conds {
		var x float64
		switch {
		case c.Answer != "":
			x = prob(c.Answer)
		case c.Op == "<=" || c.Op == "<":
			x = -a.Score
		default:
			x = a.Score
		}
		s = max(s, x)
	}
	return s
}

// average combines answers to one question, weighting each.
func average(answers []answer, weights []float64) answer {
	out := answers[0]
	out.Probabilities = map[string]float64{}
	out.Noul, out.Score, out.Confidence = 0, 0, 0
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		return answers[0]
	}
	for i, a := range answers {
		w := weights[i] / total
		out.Noul += w * a.Noul
		out.Confidence += w * a.Confidence
		for k, p := range a.Probabilities {
			out.Probabilities[k] += w * p
		}
	}
	switch out.Type {
	case "choice":
		best := ""
		for k, p := range out.Probabilities {
			if best == "" || p > out.Probabilities[best] || p == out.Probabilities[best] && k < best {
				best = k
			}
		}
		out.Choice = best
	case "score":
		for k, p := range out.Probabilities {
			if level, err := strconv.Atoi(k); err == nil {
				out.Score += float64(level) * p
			}
		}
	}
	return out
}

// lineCount counts the lines in a range such as "120-260" or "7".
func lineCount(lines string) int {
	from, to, ok := strings.Cut(lines, "-")
	a, err1 := strconv.Atoi(from)
	if !ok {
		to = from
	}
	b, err2 := strconv.Atoi(to)
	if err1 != nil || err2 != nil || b < a {
		return 1
	}
	return b - a + 1
}
