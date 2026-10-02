package skill

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide"
)

// DefaultFlagAt is how likely a flagged answer must be, when a flag does
// not say. It sits above the 40–60% band where the model is unsure.
const DefaultFlagAt = 0.6

// Flag marks the answers to a question that need attention. Each condition
// is a string: "yes" or "no" for a noul question, an option name for a
// choice question, either one followed by a threshold such as ">= 80%", or
// a comparison such as "<= 1.5" for a score question. A question is flagged
// when any of its conditions holds.
//
// In skill.json a flag is a string, or a list of strings.
type Flag []string

func (f *Flag) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) > 0 && data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = Flag{s}
		return nil
	case len(data) > 0 && data[0] == '[':
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return errors.New(`a flag list must hold strings, like ["negative", "mixed"]`)
		}
		*f = list
		return nil
	case len(data) > 0 && data[0] == '{':
		return errors.New(`flag options are not supported yet; write a flag as a string like "yes" or "<= 1.5"`)
	}
	return errors.New(`a flag must be a string like "yes" or "<= 1.5", or a list of strings`)
}

func (f Flag) MarshalJSON() ([]byte, error) {
	var v any = []string(f)
	if len(f) == 1 {
		v = f[0]
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep "<= 1.5" readable in copied skills
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(b.Bytes()), nil
}

// Condition is one parsed flag condition. It holds when the value compared
// (the probability of Answer, or the score when Answer is empty) satisfies
// Op Value.
type Condition struct {
	Answer string  // "yes", "no", or a choice option; empty for a score
	Op     string  // ">=", ">", "<=", or "<"
	Value  float64 // a probability from 0 to 1, or a score level
}

// Holds reports whether the condition is true, given the probability of
// each answer or the score.
func (c Condition) Holds(prob func(answer string) float64, score float64) bool {
	x := score
	if c.Answer != "" {
		x = prob(c.Answer)
	}
	switch c.Op {
	case ">=":
		return x >= c.Value
	case ">":
		return x > c.Value
	case "<=":
		return x <= c.Value
	case "<":
		return x < c.Value
	}
	return false
}

var thresholdPattern = regexp.MustCompile(`^(.*?)\s*(>=|>)\s*([0-9.]+)\s*%$`)

var ops = []string{">=", "<=", ">", "<"} // two-character operators first

// Conditions parses the flag for a question of type typ ("noul", "choice",
// or "score").
func (f Flag) Conditions(typ string) ([]Condition, error) {
	if len(f) == 0 {
		return nil, errors.New("flag is empty")
	}
	out := make([]Condition, 0, len(f))
	for _, s := range f {
		c, err := parseCondition(s, typ)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseCondition(s, typ string) (Condition, error) {
	s = strings.TrimSpace(s)
	if typ == "score" {
		for _, op := range ops {
			if rest, ok := strings.CutPrefix(s, op); ok {
				v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
				if err == nil {
					return Condition{Op: op, Value: v}, nil
				}
			}
		}
		return Condition{}, fmt.Errorf(`flag %q should compare the score, like "<= 1.5" or ">= 3"`, s)
	}
	example := `"yes" or "no >= 80%"`
	if typ == "choice" {
		example = `an option name, like "negative" or "negative >= 80%"`
	}
	// The threshold comes last, so option names may contain spaces.
	c := Condition{Answer: s, Op: ">=", Value: DefaultFlagAt}
	if m := thresholdPattern.FindStringSubmatch(s); m != nil {
		v, err := strconv.ParseFloat(m[3], 64)
		if err != nil || v > 100 {
			return Condition{}, fmt.Errorf("flag %q should be %s", s, example)
		}
		c = Condition{Answer: m[1], Op: m[2], Value: v / 100}
	}
	if c.Answer == "" || strings.ContainsAny(c.Answer, "<>=%") {
		return Condition{}, fmt.Errorf("flag %q should be %s", s, example)
	}
	return c, nil
}

// checkFlags validates each flag against the question it names.
func (s *Skill) checkFlags(questions map[string]decide.Question) error {
	for key, flag := range s.Flags {
		q, ok := questions[key]
		if !ok {
			return fmt.Errorf("flags names %q, which is not a question", key)
		}
		conds, err := flag.Conditions(q.QuestionType())
		if err != nil {
			return fmt.Errorf("question %q: %w", key, err)
		}
		for _, c := range conds {
			if err := checkCondition(c, q); err != nil {
				return fmt.Errorf("question %q: %w", key, err)
			}
		}
	}
	return nil
}

func checkCondition(c Condition, q decide.Question) error {
	switch q := q.(type) {
	case *decide.NoulQuestion:
		if c.Answer != "yes" && c.Answer != "no" {
			return fmt.Errorf(`a yes-or-no question is flagged with "yes" or "no", not %q`, c.Answer)
		}
	case *decide.ChoiceQuestion:
		keys := make([]string, len(q.Criteria))
		for i, o := range q.Criteria {
			keys[i] = o.Key
		}
		if !slices.Contains(keys, c.Answer) {
			return fmt.Errorf("flag %q is not an option; the options are %s", c.Answer, strings.Join(keys, ", "))
		}
	case *decide.ScoreQuestion:
		if top := len(q.Criteria) - 1; top > 0 && (c.Value < 0 || c.Value > float64(top)) {
			return fmt.Errorf("flag %s %g is outside the scale of 0 to %d", c.Op, c.Value, top)
		}
	}
	return nil
}
