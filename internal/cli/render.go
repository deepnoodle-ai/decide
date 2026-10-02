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
}

func newPrinter(w io.Writer, s *skill.Skill, details bool) *printer {
	p := &printer{w: w, skill: s, details: details}
	for _, q := range s.Questions {
		p.width = max(p.width, len(q.Key))
	}
	return p
}

func (p *printer) result(res runs.Result) error {
	var b strings.Builder
	b.WriteString(bold(clean(res.Source)))
	if preview := previewOf(res.Input, 72-len(res.Source)); preview != "" {
		b.WriteString("  " + dim(preview))
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
		fmt.Fprintf(&b, "  %-*s  %s\n", p.width, q.Key, value(p.summary(a)))
		if p.details {
			p.distribution(&b, a)
		}
	}
	b.WriteByte('\n')
	_, err := io.WriteString(p.w, b.String())
	return err
}

// summary is the one-line form of an answer.
func (p *printer) summary(a answer) string {
	switch a.Type {
	case "noul":
		return fmt.Sprintf("%s yes", percent(a.Noul))
	case "choice":
		return fmt.Sprintf("%s  %s", clean(a.Choice), percent(a.Probabilities[a.Choice]))
	case "score":
		top := levels(a) - 1
		s := fmt.Sprintf("%.1f of %d", a.Score, top)
		if label := legend(a, int(math.Round(a.Score))); label != "" {
			s += "  " + label
		}
		return s
	}
	return "(answer type " + a.Type + ")"
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
