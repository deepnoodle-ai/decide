package skill

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const flagSkill = `{"name":"x","description":"d","input":"record","questions":{
	"risky": {"type":"noul","instructions":"?"},
	"mood": {"type":"choice","instructions":"?","criteria":{"good":"g","bad":"b","mixed":"m","very bad":"v"}},
	"clarity": {"type":"score","instructions":"?","criteria":["a","b","c","d","e"]}},
	"flags": %s}`

func TestFlagsParse(t *testing.T) {
	s, err := parse([]byte(strings.Replace(flagSkill, "%s",
		`{"risky": "yes >= 80%", "mood": ["bad", "mixed > 50%", "very bad", "very bad>=90%"], "clarity": "<= 1.5"}`, 1)), "test")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string][]Condition{
		"risky":   {{Answer: "yes", Op: ">=", Value: 0.8}},
		"mood":    {{Answer: "bad", Op: ">=", Value: DefaultFlagAt}, {Answer: "mixed", Op: ">", Value: 0.5}, {Answer: "very bad", Op: ">=", Value: DefaultFlagAt}, {Answer: "very bad", Op: ">=", Value: 0.9}},
		"clarity": {{Op: "<=", Value: 1.5}},
	} {
		typ := map[string]string{"risky": "noul", "mood": "choice", "clarity": "score"}[key]
		got, err := s.Flags[key].Conditions(typ)
		if err != nil || len(got) != len(want) {
			t.Fatalf("%s: %v, %v", key, got, err)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d] = %+v, want %+v", key, i, got[i], want[i])
			}
		}
	}
	// A single condition is written back as a string, a list as a list.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(s.Flags)
	b := buf.Bytes()
	if !strings.Contains(string(b), `"risky":"yes >= 80%"`) || !strings.Contains(string(b), `"mood":["bad","mixed > 50%","very bad","very bad>=90%"]`) {
		t.Fatalf("marshal = %s", b)
	}
}

func TestFlagsExplainProblems(t *testing.T) {
	for flags, want := range map[string]string{
		`{"nope": "yes"}`:          `"nope", which is not a question`,
		`{"risky": "maybe"}`:       `"yes" or "no", not "maybe"`,
		`{"risky": "yes >= 180%"}`: `should be "yes" or "no >= 80%"`,
		`{"risky": "yes <= 20%"}`:  `should be "yes" or "no >= 80%"`,
		`{"mood": "angry"}`:        "the options are good, bad, mixed, very bad",
		`{"clarity": "low"}`:       `like "<= 1.5"`,
		`{"clarity": ">= 7"}`:      "outside the scale of 0 to 4",
		`{"risky": {"if": "yes"}}`: "not supported yet",
		`{"risky": 1}`:             "a flag must be a string",
		`{"risky": []}`:            "flag is empty",
	} {
		_, err := parse([]byte(strings.Replace(flagSkill, "%s", flags, 1)), "test")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("flags %s: %v, want %q", flags, err, want)
		}
	}
}

func TestMatchesExplainProblems(t *testing.T) {
	matchSkill := strings.Replace(flagSkill, `"flags": %s`, `"matches": %s`, 1)
	for matches, want := range map[string]string{
		`{"nope": "yes"}`:          `matches names "nope", which is not a question`,
		`{"risky": "maybe"}`:       `a yes-or-no question is matched with "yes" or "no", not "maybe"`,
		`{"risky": "yes >= 180%"}`: `match "yes >= 180%" should be`,
		`{"mood": "angry"}`:        `match "angry" is not an option`,
		`{"clarity": "low"}`:       `match "low" should compare the score`,
		`{"clarity": ">= 7"}`:      "match >= 7 is outside the scale",
		`{"risky": {"if": "yes"}}`: "match options are not supported yet",
		`{"risky": 1}`:             "a match must be a string",
		`{"risky": []}`:            "match is empty",
	} {
		_, err := parse([]byte(strings.Replace(matchSkill, "%s", matches, 1)), "test")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("matches %s: %v, want %q", matches, err, want)
		}
	}
}

func TestHolds(t *testing.T) {
	prob := func(a string) float64 { return map[string]float64{"yes": 0.7, "no": 0.3}[a] }
	for _, tc := range []struct {
		c    Condition
		want bool
	}{
		{Condition{Answer: "yes", Op: ">=", Value: 0.6}, true},
		{Condition{Answer: "yes", Op: ">=", Value: 0.8}, false},
		{Condition{Answer: "no", Op: ">=", Value: 0.6}, false},
		{Condition{Op: "<=", Value: 1.5}, true},
		{Condition{Op: "<", Value: 1.2}, false},
	} {
		if got := tc.c.Holds(prob, 1.2); got != tc.want {
			t.Errorf("%+v holds = %v", tc.c, got)
		}
	}
}

func TestCopyKeepsFlags(t *testing.T) {
	isolate(t)
	from, _ := Load("code-risk")
	if _, err := Create("mine", from, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(ProjectDir, "mine", "skill.json"))
	if !strings.Contains(string(data), `"maintainability": "<= 1.5"`) {
		t.Fatalf("copy lost its flags:\n%s", data)
	}
}
