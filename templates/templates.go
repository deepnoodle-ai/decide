// Package templates gives Go programs the decide command's built-in
// templates, the same ones decide templates lists. A template is a named set
// of questions and the answers it flags. Use this package to ask a built-in
// template's questions about one item, and to flag the answers as decide run
// does. It does not split data into items, apply matches, or load your own
// templates. When decide tunes a template's wording or flags, a program gets
// the change by upgrading decide.
//
// Each built-in template is a variable, such as [CommandRisk]. Its typed
// accessors return a new question for [decide.Ask]:
//
//	req := decide.NewRequest(command)
//	severe := decide.Ask(req, "severe", templates.CommandRisk.Noul("severe"))
//	resp, err := client.SystemOne(ctx, req)
//	if err != nil {
//		return err
//	}
//	a, err := severe.From(resp)
//	if err != nil || templates.CommandRisk.Flagged("severe", a) {
//		// No usable answer, or yes is at least 80% likely: stop the command.
//	}
//
// A template's parameters take their defaults. [Relevance] has a parameter
// with no default; set it with [Template.With] before asking its question.
// [ReceiptQuality] reads images: attach them as the provider expects, such
// as with cloudflare.SetImages.
//
// The package exports flags, not the matches a template such as Relevance
// uses to pick items out; a program applies its own rule to those answers.
//
// The package reads no files and no environment variables. Templates in
// .decide/templates or DECIDE_HOME do not replace these.
package templates

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/template"
)

// The built-in templates. Run decide templates show <name> to read one.
var (
	CodeRisk        = builtin("code-risk")
	CommandRisk     = builtin("command-risk")
	PRDescription   = builtin("pr-description")
	PromptInjection = builtin("prompt-injection")
	ReceiptQuality  = builtin("receipt-quality")
	Relevance       = builtin("relevance")
	Security        = builtin("security")
	Sentiment       = builtin("sentiment")
	TaskReadiness   = builtin("task-readiness")
	TicketRouting   = builtin("ticket-routing")
	Triage          = builtin("triage")
)

// All returns the built-in templates, sorted by name.
func All() []*Template {
	return []*Template{CodeRisk, CommandRisk, PRDescription, PromptInjection, ReceiptQuality,
		Relevance, Security, Sentiment, TaskReadiness, TicketRouting, Triage}
}

// Template is a built-in template: a named set of questions and the answers
// it flags. It is immutable and safe for concurrent use.
type Template struct {
	t     *template.Template // with parameter values in place
	base  *template.Template // as loaded, with its placeholders
	flags map[string][]template.Condition
	types map[string]string // question type by key, for the flagged questions
}

func builtin(name string) *Template {
	t, err := template.LoadBuiltin(name)
	if err != nil {
		panic(fmt.Sprintf("decide: built-in template %s: %v", name, err))
	}
	return newTemplate(t, t.With(nil))
}

func newTemplate(base, t *template.Template) *Template {
	out := &Template{t: t, base: base, flags: map[string][]template.Condition{}, types: map[string]string{}}
	for key, flag := range t.Flags {
		typ := ""
		if q := out.Question(key); q != nil {
			typ = q.QuestionType()
		}
		out.types[key] = typ
		conds, err := flag.Conditions(typ)
		if err != nil {
			panic(fmt.Sprintf("decide: template %s flag %q: %v", t.Name, key, err)) // Validate checked it
		}
		out.flags[key] = conds
	}
	return out
}

// Name returns the template's name, such as "command-risk".
func (t *Template) Name() string { return t.t.Name }

// Description says what the template decides about each item.
func (t *Template) Description() string { return t.t.Description }

// Keys returns the question keys in the order the template asks them.
func (t *Template) Keys() []string {
	keys := make([]string, len(t.t.Questions))
	for i, q := range t.t.Questions {
		keys[i] = q.Key
	}
	return keys
}

// Question returns a new copy of the question named key, or nil when the
// template has no such question.
func (t *Template) Question(key string) decide.Question {
	for _, q := range t.t.Questions {
		if q.Key == key {
			typed, err := decide.DecodeQuestion(q.Raw)
			if err != nil {
				return nil // Validate decoded every question
			}
			return typed
		}
	}
	return nil
}

// Noul returns a new copy of the yes-or-no question named key, or nil when
// key names no such question.
func (t *Template) Noul(key string) *decide.NoulQuestion {
	q, _ := t.Question(key).(*decide.NoulQuestion)
	return q
}

// Choice returns a new copy of the choice question named key, or nil when
// key names no such question.
func (t *Template) Choice(key string) *decide.ChoiceQuestion {
	q, _ := t.Question(key).(*decide.ChoiceQuestion)
	return q
}

// Score returns a new copy of the score question named key, or nil when key
// names no such question.
func (t *Template) Score(key string) *decide.ScoreQuestion {
	q, _ := t.Question(key).(*decide.ScoreQuestion)
	return q
}

// Flag returns the template's flag for the question named key, as written
// in template.json, such as "yes >= 80%". A condition with no threshold,
// such as "no", holds when that answer is at least 60% likely. It is nil
// when the template does not flag that question.
func (t *Template) Flag(key string) []string {
	return slices.Clone(t.t.Flags[key])
}

// Flagged reports whether the template flags a, the answer to the question
// named key, as decide run does. It is false when the template does not
// flag that question, or when a is nil or not an answer of that question's
// type.
func (t *Template) Flagged(key string, a decide.Answer) bool {
	var prob func(string) float64
	var score float64
	switch a := a.(type) {
	case *decide.NoulAnswer:
		if a == nil || t.types[key] != "noul" {
			return false
		}
		prob = func(answer string) float64 {
			if answer == "yes" {
				return a.Noul
			}
			return 1 - a.Noul
		}
	case *decide.ChoiceAnswer:
		if a == nil || t.types[key] != "choice" {
			return false
		}
		prob = func(answer string) float64 { return a.Probabilities[answer] }
	case *decide.ScoreAnswer:
		if a == nil || t.types[key] != "score" {
			return false
		}
		prob, score = func(string) float64 { return 0 }, a.Score
	default:
		return false
	}
	for _, c := range t.flags[key] {
		if c.Holds(prob, score) {
			return true
		}
	}
	return false
}

// Parameters returns the names of the template's parameters, sorted. A
// parameter fills a {{name}} placeholder in the questions.
func (t *Template) Parameters() []string {
	return slices.Sorted(maps.Keys(t.t.Parameters))
}

// With returns a copy of the template with its parameters set to values.
// A parameter missing from values keeps its default. It is an error to name
// a parameter the template does not have, or to leave one with no default
// unset. With starts from the built-in template, so values from an earlier
// With do not carry over.
func (t *Template) With(values map[string]string) (*Template, error) {
	resolved, err := t.base.Resolve(values)
	if err != nil {
		return nil, errors.New(err.Error()) // not an internal type
	}
	return newTemplate(t.base, resolved), nil
}
