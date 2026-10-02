package skill

import (
	"bytes"
	"encoding/json"
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

// Match marks the answers to a question that someone is looking for. It is
// written like a Flag.
type Match []string

// mark names a kind of condition in error messages.
type mark struct{ noun, verb string }

var (
	flagMark  = mark{"flag", "flagged"}
	matchMark = mark{"match", "matched"}
)

func (f *Flag) UnmarshalJSON(data []byte) error {
	return unmarshalMark((*[]string)(f), data, flagMark)
}

func (m *Match) UnmarshalJSON(data []byte) error {
	return unmarshalMark((*[]string)(m), data, matchMark)
}

func unmarshalMark(out *[]string, data []byte, k mark) error {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) > 0 && data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*out = []string{s}
		return nil
	case len(data) > 0 && data[0] == '[':
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf(`a %s list must hold strings, like ["negative", "mixed"]`, k.noun)
		}
		*out = list
		return nil
	case len(data) > 0 && data[0] == '{':
		return fmt.Errorf(`%s options are not supported yet; write a %s as a string like "yes" or "<= 1.5"`, k.noun, k.noun)
	}
	return fmt.Errorf(`a %s must be a string like "yes" or "<= 1.5", or a list of strings`, k.noun)
}

func (f Flag) MarshalJSON() ([]byte, error) { return marshalMark(f) }

func (m Match) MarshalJSON() ([]byte, error) { return marshalMark(m) }

func marshalMark(conds []string) ([]byte, error) {
	var v any = conds
	if len(conds) == 1 {
		v = conds[0]
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
func (f Flag) Conditions(typ string) ([]Condition, error) { return conditions(f, typ, flagMark) }

// Conditions parses the match for a question of type typ.
func (m Match) Conditions(typ string) ([]Condition, error) { return conditions(m, typ, matchMark) }

func conditions(list []string, typ string, k mark) ([]Condition, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("%s is empty", k.noun)
	}
	out := make([]Condition, 0, len(list))
	for _, s := range list {
		c, err := parseCondition(s, typ, k)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseCondition(s, typ string, k mark) (Condition, error) {
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
		return Condition{}, fmt.Errorf(`%s %q should compare the score, like "<= 1.5" or ">= 3"`, k.noun, s)
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
			return Condition{}, fmt.Errorf("%s %q should be %s", k.noun, s, example)
		}
		c = Condition{Answer: m[1], Op: m[2], Value: v / 100}
	}
	if c.Answer == "" || strings.ContainsAny(c.Answer, "<>=%") {
		return Condition{}, fmt.Errorf("%s %q should be %s", k.noun, s, example)
	}
	return c, nil
}

// checkFlags validates each flag and match against the question it names.
func (s *Skill) checkFlags(questions map[string]decide.Question) error {
	if err := checkMarks("flags", s.Flags, flagMark, questions); err != nil {
		return err
	}
	return checkMarks("matches", s.Matches, matchMark, questions)
}

func checkMarks[M ~[]string](field string, marks map[string]M, k mark, questions map[string]decide.Question) error {
	for key, list := range marks {
		q, ok := questions[key]
		if !ok {
			return fmt.Errorf("%s names %q, which is not a question", field, key)
		}
		conds, err := conditions(list, q.QuestionType(), k)
		if err != nil {
			return fmt.Errorf("question %q: %w", key, err)
		}
		for _, c := range conds {
			if err := checkCondition(c, q, k); err != nil {
				return fmt.Errorf("question %q: %w", key, err)
			}
		}
	}
	return nil
}

func checkCondition(c Condition, q decide.Question, k mark) error {
	switch q := q.(type) {
	case *decide.NoulQuestion:
		if c.Answer != "yes" && c.Answer != "no" {
			return fmt.Errorf(`a yes-or-no question is %s with "yes" or "no", not %q`, k.verb, c.Answer)
		}
	case *decide.ChoiceQuestion:
		keys := make([]string, len(q.Criteria))
		for i, o := range q.Criteria {
			keys[i] = o.Key
		}
		if !slices.Contains(keys, c.Answer) {
			return fmt.Errorf("%s %q is not an option; the options are %s", k.noun, c.Answer, strings.Join(keys, ", "))
		}
	case *decide.ScoreQuestion:
		if top := len(q.Criteria) - 1; top > 0 && (c.Value < 0 || c.Value > float64(top)) {
			return fmt.Errorf("%s %s %g is outside the scale of 0 to %d", k.noun, c.Op, c.Value, top)
		}
	}
	return nil
}
