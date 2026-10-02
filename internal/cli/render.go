package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/deepnoodle-ai/decide/internal/runs"
	"github.com/deepnoodle-ai/decide/internal/skill"
	"github.com/deepnoodle-ai/wonton/color"
)

// Text styles. They print plain text when color is off.
func bold(s string) string   { return color.ApplyBold(s) }
func dim(s string) string    { return color.ApplyDim(s) }
func value(s string) string  { return color.Cyan.Apply(s) }
func failed(s string) string { return color.Red.Apply(s) }
func good(s string) string   { return color.Green.Apply(s) }
func warn(s string) string   { return color.Yellow.Apply(s) }

// A yes-or-no answer between these probabilities is shown as unsure.
const (
	unsureAbove = 0.4
	unsureBelow = 0.6
)

// answer is the stored JSON form of a noul, choice, or score answer.
type answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul"`
	Choice        string             `json:"choice"`
	Score         float64            `json:"score"`
	Legend        map[string]any     `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// printer writes results for people to read.
type printer struct {
	w       io.Writer
	skill   *skill.Skill
	details bool
	width   int // width of the question-name column
	answers int // width of the answer column
	marks   marks
}

func newPrinter(w io.Writer, s *skill.Skill, details bool) *printer {
	p := &printer{w: w, skill: s, details: details, marks: marksOf(s)}
	for _, q := range s.Questions {
		p.width = max(p.width, len(q.Key))
		p.answers = max(p.answers, answerWidth(q.Raw))
	}
	return p
}

// answerWidth is the widest answer a question can have, so the figures
// after the answers line up across questions and items.
func answerWidth(raw json.RawMessage) int {
	var q struct {
		Type     string          `json:"type"`
		Criteria json.RawMessage `json:"criteria"`
	}
	json.Unmarshal(raw, &q)
	switch q.Type {
	case "score":
		return trackWidth
	case "choice":
		var opts skill.Questions
		w := 0
		if json.Unmarshal(q.Criteria, &opts) == nil {
			for _, o := range opts {
				w = max(w, len([]rune(o.Key)))
			}
		} else {
			var list []string
			json.Unmarshal(q.Criteria, &list)
			for _, o := range list {
				w = max(w, len([]rune(o)))
			}
		}
		return min(w, 24)
	}
	return len("unsure")
}

// item prints one item's answers.
func (p *printer) item(it item) error {
	res := it.Result
	var b strings.Builder
	b.WriteString(bold(clean(res.Source)))
	if preview := previewOf(res.Input, 72-len(res.Source)); preview != "" {
		b.WriteString("  " + dim(preview))
	}
	if it.parts > 0 {
		b.WriteString("  " + dim(fmt.Sprintf("judged in %d parts", it.parts)))
	}
	b.WriteByte('\n')
	if res.Status != "complete" {
		fmt.Fprintf(&b, "  %s\n", failed("✗ "+friendlyError(res.Error)))
	}
	for _, q := range p.skill.Questions {
		raw, ok := res.Answers[q.Key]
		if !ok {
			continue
		}
		var a answer
		if err := json.Unmarshal(raw, &a); err != nil {
			continue
		}
		v := p.marks.judge(q.Key, a)
		gutter := "  "
		switch v {
		case flagged:
			gutter = failed("!") + " "
		case matched:
			gutter = good("●") + " "
		}
		line := p.summary(a, v)
		if w := it.where[q.Key]; w != "" {
			line += "  " + dim(clean(w))
		}
		fmt.Fprintf(&b, "%s%-*s  %s\n", gutter, p.width, clean(q.Key), line)
		if p.details {
			p.distribution(&b, a)
		}
	}
	b.WriteByte('\n')
	_, err := io.WriteString(p.w, b.String())
	return err
}

// summary is the one-line form of an answer, styled by its verdict.
// Every answer has the same columns: the answer (a word, or a track for a
// score), a figure, and for scores the level's description.
func (p *printer) summary(a answer, v verdict) string {
	var ans, figure, note string
	switch a.Type {
	case "noul":
		switch {
		case a.Noul >= unsureBelow:
			ans, figure = v.style("yes"), v.style(percent(a.Noul))
		case a.Noul <= unsureAbove:
			ans, figure = v.style("no"), v.style(percent(1-a.Noul))
		default:
			ans, figure = v.style("unsure"), v.style(percent(a.Noul)+" yes")
		}
	case "choice":
		ans, figure = v.style(clean(a.Choice)), v.style(percent(a.Probabilities[a.Choice]))
	case "score":
		top := levels(a) - 1
		ans, figure = track(a.Score, top, v), v.style(fmt.Sprintf("%.1f of %d", a.Score, top))
		note = legend(a, int(math.Round(a.Score)))
	default:
		return value("(answer type " + clean(a.Type) + ")")
	}
	s := pad(ans, p.answers) + "  " + figure
	if note != "" {
		s += "  " + dim(note)
	}
	return s
}

func (p *printer) distribution(b *strings.Builder, a answer) {
	indent := strings.Repeat(" ", p.width+4)
	switch a.Type {
	case "noul":
		fmt.Fprintf(b, "%s%s %s yes · %s no\n", indent, bar(a.Noul), percent(a.Noul), percent(1-a.Noul))
		return
	case "choice":
		keys := make([]string, 0, len(a.Probabilities))
		w := 0
		for k := range a.Probabilities {
			keys = append(keys, k)
			w = max(w, len(k))
		}
		slices.SortFunc(keys, func(x, y string) int {
			if c := cmpFloat(a.Probabilities[y], a.Probabilities[x]); c != 0 {
				return c
			}
			return strings.Compare(x, y)
		})
		for _, k := range keys {
			fmt.Fprintf(b, "%s%-*s  %s %4s\n", indent, w, clean(k), bar(a.Probabilities[k]), percent(a.Probabilities[k]))
		}
	case "score":
		for i := range levels(a) {
			pr := a.Probabilities[strconv.Itoa(i)]
			fmt.Fprintf(b, "%s%d  %s %4s  %s\n", indent, i, bar(pr), percent(pr), dim(truncate(legend(a, i), 50)))
		}
	}
	fmt.Fprintf(b, "%s%s\n", indent, dim("confidence "+percent(a.Confidence)))
}

// verdict is how an answer reads against its question's flag or match.
type verdict int

const (
	plain     verdict = iota // no flag or match, and nothing to point out
	clear                    // the flag does not apply
	near                     // the model is unsure, or a flag or match is possible
	flagged                  // the answer needs attention
	matched                  // the answer is one the user is looking for
	unmatched                // the match does not apply
)

func (v verdict) style(s string) string {
	switch v {
	case clear:
		return good(s)
	case matched:
		return bold(good(s))
	case unmatched:
		return dim(s)
	case near:
		return warn(s)
	case flagged:
		return failed(s)
	}
	return value(s)
}

// judge compares an answer with its question's flag and match conditions.
// A flag outweighs a match.
func judge(flag, match []skill.Condition, a answer) verdict {
	prob := probOf(a)
	for _, c := range flag {
		if c.Holds(prob, a.Score) {
			return flagged
		}
	}
	for _, c := range match {
		if c.Holds(prob, a.Score) {
			return matched
		}
	}
	v := plain
	switch {
	case len(flag) > 0:
		v = clear
	case len(match) > 0:
		v = unmatched
	}
	for _, c := range append(slices.Clone(flag), match...) {
		if c.Answer != "" && prob(c.Answer) > unsureAbove {
			v = near
		}
	}
	if a.Type == "noul" && a.Noul > unsureAbove && a.Noul < unsureBelow {
		v = near
	}
	return v
}

// probOf returns the probability of each answer: "yes" or "no", or a
// choice option.
func probOf(a answer) func(string) float64 {
	return func(answer string) float64 {
		if a.Type == "noul" {
			if answer == "yes" {
				return a.Noul
			}
			return 1 - a.Noul
		}
		return a.Probabilities[answer]
	}
}

// marks are a skill's parsed flags and matches, by question.
type marks struct {
	flags, matches map[string][]skill.Condition
}

func marksOf(s *skill.Skill) marks {
	return marks{conditionsOf(s, s.Flags), conditionsOf(s, s.Matches)}
}

func (m marks) judge(key string, a answer) verdict {
	return judge(m.flags[key], m.matches[key], a)
}

// has reports whether any answer in a result has the verdict v.
func (m marks) has(res runs.Result, v verdict) bool {
	for key, raw := range res.Answers {
		var a answer
		if json.Unmarshal(raw, &a) == nil && m.judge(key, a) == v {
			return true
		}
	}
	return false
}

// conditionsOf parses a skill's flags or matches. Skills are validated when
// they load, so a flag that does not parse is ignored.
func conditionsOf[F interface {
	Conditions(typ string) ([]skill.Condition, error)
}](s *skill.Skill, flags map[string]F) map[string][]skill.Condition {
	out := map[string][]skill.Condition{}
	for _, q := range s.Questions {
		flag, ok := flags[q.Key]
		if !ok {
			continue
		}
		var body struct {
			Type string `json:"type"`
		}
		json.Unmarshal(q.Raw, &body)
		if conds, err := flag.Conditions(body.Type); err == nil {
			out[q.Key] = conds
		}
	}
	return out
}

// flagText describes flag conditions in words, like "yes is 60% or more
// likely" or "the score is 1.5 or lower".
func flagText(conds []skill.Condition) string {
	parts := make([]string, len(conds))
	for i, c := range conds {
		if c.Answer == "" {
			bound := map[string]string{">=": "%s or higher", ">": "above %s", "<=": "%s or lower", "<": "below %s"}[c.Op]
			parts[i] = "the score is " + fmt.Sprintf(bound, strconv.FormatFloat(c.Value, 'g', -1, 64))
			continue
		}
		bound := map[string]string{">=": "%s or more likely", ">": "more than %s likely"}[c.Op]
		parts[i] = c.Answer + " is " + fmt.Sprintf(bound, percent(c.Value))
	}
	return strings.Join(parts, ", or ")
}

// trackWidth is the number of cells in a score's track.
const trackWidth = 12

// track draws a score as a line filled in half-cell steps, such as
// "━━━━━━━╸────" for 2.5 of 4. Heavy and light lines tell the filled part
// from the rest when color is off.
func track(score float64, top int, v verdict) string {
	halves := 0
	if top > 0 {
		halves = max(0, min(2*trackWidth, int(math.Round(score/float64(top)*2*trackWidth))))
	}
	filled := strings.Repeat("━", halves/2)
	if halves%2 == 1 {
		filled += "╸"
	}
	if filled != "" {
		filled = v.style(filled)
	}
	return filled + dim(strings.Repeat("─", trackWidth-(halves+1)/2))
}

func cmpFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func levels(a answer) int { return max(len(a.Probabilities), len(a.Legend)) }

func legend(a answer, i int) string {
	switch v := a.Legend[strconv.Itoa(i)].(type) {
	case string:
		return clean(v)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return clean(string(b))
	}
}

func percent(p float64) string { return fmt.Sprintf("%.0f%%", p*100) }

func bar(p float64) string {
	filled := max(0, min(10, int(math.Round(p*10))))
	return value(strings.Repeat("█", filled)) + dim(strings.Repeat("░", 10-filled))
}

// previewOf shows the start of a record so results are recognizable.
func previewOf(input json.RawMessage, room int) string {
	if len(input) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(input, &text) != nil {
		text = string(input)
	}
	return truncate(clean(text), max(room, 24))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// clean flattens control characters, so data cannot move the cursor or
// change terminal colors.
func clean(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}), " ")
}

func friendlyError(msg string) string {
	msg = strings.TrimPrefix(msg, "decide: ")
	if msg == "" {
		return "failed"
	}
	return clean(msg)
}

// describe explains a question type in plain words.
func describe(raw json.RawMessage) string {
	var q struct {
		Type     string          `json:"type"`
		Criteria json.RawMessage `json:"criteria"`
	}
	json.Unmarshal(raw, &q)
	switch q.Type {
	case "noul":
		return "yes or no"
	case "choice":
		var opts skill.Questions
		if json.Unmarshal(q.Criteria, &opts) == nil {
			keys := make([]string, len(opts))
			for i, o := range opts {
				keys[i] = o.Key
			}
			return "one of " + strings.Join(keys, ", ")
		}
		var list []string
		json.Unmarshal(q.Criteria, &list)
		return "one of " + strings.Join(list, ", ")
	case "score":
		var levels []json.RawMessage
		json.Unmarshal(q.Criteria, &levels)
		return fmt.Sprintf("a score from 0 to %d", max(len(levels)-1, 0))
	}
	return q.Type
}

// wrap breaks text into lines of at most width runes, each with indent.
func wrap(text string, width int, indent string) string {
	var out, line strings.Builder
	for _, word := range strings.Fields(text) {
		if line.Len() > 0 && line.Len()+1+len(word) > width {
			out.WriteString(indent + line.String() + "\n")
			line.Reset()
		}
		if line.Len() > 0 {
			line.WriteByte(' ')
		}
		line.WriteString(word)
	}
	if line.Len() > 0 {
		out.WriteString(indent + line.String() + "\n")
	}
	return out.String()
}
