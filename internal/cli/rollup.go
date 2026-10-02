package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
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
	where       map[string]string // for each question, the part that decided it, such as "lines 3-9"
}

// group collects saved results into items. The parts of an item are
// consecutive. An item missing a part is left out, unless a part it has
// failed: then the item failed.
func group(results []runs.Result, m marks) []item {
	var out []item
	for i := 0; i < len(results); i++ {
		res := results[i]
		if res.Part == nil {
			out = append(out, item{Result: res})
			continue
		}
		first := firstPart(res)
		parts := []runs.Result{}
		for ; i < len(results) && results[i].Part != nil && firstPart(results[i]) == first; i++ {
			parts = append(parts, results[i])
		}
		i--
		if len(parts) == res.Part.Of {
			out = append(out, combine(parts, m))
		} else if it, ok := failure(parts); ok {
			out = append(out, it)
		}
	}
	return out
}

// firstPart is the index of the first part of a result's item.
func firstPart(res runs.Result) int {
	if res.Part == nil {
		return res.Index
	}
	return res.Index - (res.Part.N - 1)
}

// collector turns results into items as they arrive. A part waits until
// every part of its item is in, including parts answered before a run
// was resumed, and then the item is emitted as one.
type collector struct {
	marks marks
	saved map[int]runs.Result // complete results from before this execution
	parts map[int]runs.Result // parts that arrived, until their item is whole
	emit  func(item) error
}

func newCollector(m marks, saved []runs.Result, emit func(item) error) *collector {
	c := &collector{marks: m, saved: map[int]runs.Result{}, parts: map[int]runs.Result{}, emit: emit}
	for _, res := range saved {
		if res.Status == "complete" {
			c.saved[res.Index] = res
		}
	}
	return c
}

func (c *collector) add(res runs.Result) error {
	if res.Part == nil {
		return c.emit(item{Result: res})
	}
	c.parts[res.Index] = res
	first := firstPart(res)
	parts := make([]runs.Result, res.Part.Of)
	for i := range parts {
		r, ok := c.parts[first+i]
		if !ok {
			if r, ok = c.saved[first+i]; !ok {
				return nil
			}
		}
		parts[i] = r
	}
	for i := range parts {
		delete(c.parts, first+i)
	}
	return c.emit(combine(parts, c.marks))
}

// finish emits the items still waiting for a part that have a failed
// part, so a run that stopped early still shows what failed.
func (c *collector) finish() error {
	byItem := map[int][]runs.Result{}
	var firsts []int
	for _, res := range c.parts {
		f := firstPart(res)
		if byItem[f] == nil {
			firsts = append(firsts, f)
		}
		byItem[f] = append(byItem[f], res)
	}
	slices.Sort(firsts)
	for _, f := range firsts {
		parts := byItem[f]
		slices.SortFunc(parts, func(a, b runs.Result) int { return a.Index - b.Index })
		if it, ok := failure(parts); ok {
			if err := c.emit(it); err != nil {
				return err
			}
		}
	}
	return nil
}

// failure returns the failed item of some parts of an item, if any part
// failed.
func failure(parts []runs.Result) (item, bool) {
	for _, p := range parts {
		if p.Status != "complete" {
			it := item{parts: p.Part.Of}
			it.Index, it.Source, it.Input, it.Status = firstPart(p), p.Source, p.Input, "failed"
			it.Model, it.RequestID = p.Model, p.RequestID
			it.Error = fmt.Sprintf("part %d of %d: %s", p.Part.N, p.Part.Of, friendlyError(p.Error))
			if p.Part.Lines != "" {
				it.Error = fmt.Sprintf("part %d of %d (lines %s): %s", p.Part.N, p.Part.Of, p.Part.Lines, friendlyError(p.Error))
			}
			return it, true
		}
	}
	return item{}, false
}

// jsonItem is an item as --json prints it: one line per item, with the
// parts it was judged in, and the part that decided each flagged or
// matched answer.
type jsonItem struct {
	runs.Result
	Parts int               `json:"parts,omitempty"`
	Where map[string]string `json:"where,omitempty"`
}

func (it item) json() jsonItem {
	j := jsonItem{Result: it.Result, Parts: it.parts}
	if len(it.where) > 0 {
		j.Where = it.where
	}
	return j
}

// partName names a part for people: by its lines in a file, or by its
// number in a record.
func partName(p *runs.Part) string {
	if p.Lines != "" {
		return "lines " + p.Lines
	}
	return fmt.Sprintf("part %d of %d", p.N, p.Of)
}

// combine makes one result from the results for an item's parts. A
// question with a flag takes the answer of the part closest to being
// flagged, so an item is flagged when any part is; a question with a
// match takes the part closest to matching. Other answers are averaged,
// weighted by the size of each part.
func combine(parts []runs.Result, m marks) item {
	if it, ok := failure(parts); ok {
		return it
	}
	first := parts[0]
	it := item{parts: len(parts), where: map[string]string{}}
	it.Index, it.Source, it.Input, it.Status = first.Index, first.Source, first.Input, "complete"
	it.Model, it.RequestID = first.Model, first.RequestID
	it.Answers = map[string]json.RawMessage{}
	for key := range first.Answers {
		answers := make([]answer, 0, len(parts))
		weights := make([]float64, 0, len(parts))
		for _, p := range parts {
			var a answer
			if raw, ok := p.Answers[key]; ok && json.Unmarshal(raw, &a) == nil {
				answers = append(answers, a)
				weights = append(weights, weight(p.Part))
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
			it.where[key] = partName(parts[best].Part)
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

// weight is how much a part counts in an average: its size, or for a run
// saved before sizes were, its number of lines.
func weight(p *runs.Part) float64 {
	if p.Size > 0 {
		return float64(p.Size)
	}
	return float64(lineCount(p.Lines))
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
