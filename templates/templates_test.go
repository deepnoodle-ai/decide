package templates_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/internal/template"
	"github.com/deepnoodle-ai/decide/templates"
)

func TestAllIsEveryBuiltin(t *testing.T) {
	var names []string
	for _, tmpl := range templates.All() {
		names = append(names, tmpl.Name())
	}
	if want := template.Builtins(); !slices.Equal(names, want) {
		t.Fatalf("All() is %v, the built-in templates are %v", names, want)
	}
}

func TestQuestionsMatchTheCLI(t *testing.T) {
	for _, tmpl := range templates.All() {
		cli, err := template.LoadBuiltin(tmpl.Name())
		if err != nil {
			t.Fatal(err)
		}
		want, err := cli.With(nil).Decode()
		if err != nil {
			t.Fatal(err)
		}
		if tmpl.Description() != cli.Description || len(tmpl.Keys()) != len(want) {
			t.Errorf("%s: description or keys differ from the CLI's", tmpl.Name())
		}
		for _, key := range tmpl.Keys() {
			q := tmpl.Question(key)
			got, _ := json.Marshal(q)
			exp, _ := json.Marshal(want[key])
			if string(got) != string(exp) {
				t.Errorf("%s %s:\n got %s\nwant %s", tmpl.Name(), key, got, exp)
			}
			var typed decide.Question
			switch q.QuestionType() {
			case "noul":
				typed = tmpl.Noul(key)
			case "choice":
				typed = tmpl.Choice(key)
			case "score":
				typed = tmpl.Score(key)
			}
			if typed == nil || tmpl.Choice(key) != nil && q.QuestionType() != "choice" {
				t.Errorf("%s %s: typed accessor does not match type %s", tmpl.Name(), key, q.QuestionType())
			}
		}
	}
	if templates.Triage.Question("nope") != nil || templates.Triage.Noul("impact") != nil {
		t.Fatal("a missing key or the wrong type returns a question")
	}
}

func TestQuestionIsACopy(t *testing.T) {
	q := templates.CommandRisk.Noul("severe")
	q.Instructions = "changed"
	if templates.CommandRisk.Noul("severe").Instructions == "changed" {
		t.Fatal("changing a returned question changed the template")
	}
}

func TestFlagged(t *testing.T) {
	choice := func(p map[string]float64) *decide.ChoiceAnswer { return &decide.ChoiceAnswer{Probabilities: p} }
	for _, tc := range []struct {
		tmpl *templates.Template
		key  string
		a    decide.Answer
		want bool
	}{
		{templates.CommandRisk, "severe", &decide.NoulAnswer{Noul: 0.8}, true},
		{templates.CommandRisk, "severe", &decide.NoulAnswer{Noul: 0.79}, false},
		{templates.CommandRisk, "external", &decide.NoulAnswer{Noul: 1}, false},  // not flagged
		{templates.PRDescription, "tested", &decide.NoulAnswer{Noul: 0.3}, true}, // "no" at the default 60%
		{templates.Sentiment, "sentiment", choice(map[string]float64{"negative": 0.7}), true},
		{templates.Sentiment, "sentiment", choice(map[string]float64{"negative": 0.5}), false},
		{templates.Triage, "impact", &decide.ScoreAnswer{Score: 3}, true},
		{templates.Triage, "impact", &decide.ScoreAnswer{Score: 2.9}, false},
		{templates.CommandRisk, "severe", &decide.RawAnswer{Type: "refusal"}, false},
		{templates.CommandRisk, "severe", nil, false},
		{templates.CommandRisk, "severe", (*decide.NoulAnswer)(nil), false},
		{templates.Sentiment, "sentiment", (*decide.ChoiceAnswer)(nil), false},
		{templates.Triage, "impact", (*decide.ScoreAnswer)(nil), false},
		{templates.CodeRisk, "maintainability", &decide.NoulAnswer{Noul: 0.99}, false}, // wrong type
		{templates.CommandRisk, "severe", choice(map[string]float64{"yes": 1}), false},
	} {
		if got := tc.tmpl.Flagged(tc.key, tc.a); got != tc.want {
			t.Errorf("%s %s %+v: flagged %v, want %v", tc.tmpl.Name(), tc.key, tc.a, got, tc.want)
		}
	}
	if templates.CommandRisk.Flag("external") != nil {
		t.Fatal("an unflagged question has a flag")
	}
}

func TestWith(t *testing.T) {
	if !strings.Contains(templates.Relevance.Noul("relevant").Instructions.(string), "{{question}}") {
		t.Fatal("an unset parameter should stay a placeholder")
	}
	r, err := templates.Relevance.With(map[string]string{"question": "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Noul("relevant").Instructions.(string); !strings.Contains(s, "billing") || strings.Contains(s, "{{") {
		t.Fatalf("parameter not set: %s", s)
	}
	if _, err := r.With(nil); err == nil {
		t.Fatal("With should start from the built-in template")
	}
	if _, err := templates.Triage.With(map[string]string{"x": "y"}); err == nil {
		t.Fatal("an unknown parameter was accepted")
	}
	focus, err := templates.CodeRisk.With(map[string]string{"focus": "SQL injection"})
	if err != nil || !strings.Contains(focus.Noul("risk").Instructions.(string), "SQL injection") {
		t.Fatalf("focus not set: %v", err)
	}
	if !strings.Contains(templates.CodeRisk.Noul("risk").Instructions.(string), "authorization flaws") {
		t.Fatal("the default was not applied")
	}
}
