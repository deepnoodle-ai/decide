package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinLibraryIsExecutable(t *testing.T) {
	for n := range builtinSkills {
		s, e := LoadSkill("builtin/" + n)
		if e != nil {
			t.Fatalf("%s: %v", n, e)
		}
		s, e = ResolveParameters(s, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = Questions(s); e != nil {
			t.Fatal(e)
		}
	}
	for n := range builtinPatterns {
		if _, e := LoadPattern("builtin/" + n); e != nil {
			t.Fatalf("%s: %v", n, e)
		}
	}
}

func TestParameterSubstitutionPreservesTypesAndDoesNotMutate(t *testing.T) {
	s := Skill{Version: 1, Name: "custom", Description: "Custom", Inputs: []string{"json"}, Parameters: map[string]Parameter{"threshold": {Type: "number", Required: true}, "topic": {Type: "string", Default: json.RawMessage(`"hello"`)}}, Questions: map[string]json.RawMessage{"q": json.RawMessage(`{"type":"noul","instructions":{"threshold":"{{threshold}}","topic":"About {{topic}}"}}`)}}
	r, e := ResolveParameters(s, map[string]string{"threshold": "0.7", "topic": "a \"quote\""})
	if e != nil {
		t.Fatal(e)
	}
	var q struct {
		Instructions struct {
			Threshold float64
			Topic     string
		}
	}
	if e = json.Unmarshal(r.Questions["q"], &q); e != nil {
		t.Fatal(e)
	}
	if q.Instructions.Threshold != 0.7 || q.Instructions.Topic != `About a "quote"` {
		t.Fatalf("%+v", q)
	}
	if !strings.Contains(string(s.Questions["q"]), "{{threshold}}") {
		t.Fatal("mutated original")
	}
	for _, values := range []map[string]string{nil, {"threshold": "bad"}, {"threshold": "false"}, {"threshold": "0.5", "missing": "x"}} {
		if _, e := ResolveParameters(s, values); e == nil {
			t.Fatalf("accepted %+v", values)
		}
	}
	s.Questions["q"] = json.RawMessage(`{"type":"noul","instructions":"{{undeclared}}"}`)
	if _, e := ResolveParameters(s, map[string]string{"threshold": "0.5"}); e == nil {
		t.Fatal("accepted undeclared placeholder")
	}
}

func TestLocalLibraryCollisionCreationAndStrictConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DECIDE_HOME", filepath.Join(t.TempDir(), "home"))
	p, e := CreateSkill("project/code-risk", "builtin/code-risk")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSkill("code-risk"); e == nil {
		t.Fatal("ambiguous name selected silently")
	}
	if _, e = LoadSkill("project/code-risk"); e != nil {
		t.Fatal(e)
	}
	if _, e = SkillPath("builtin/code-risk"); e == nil {
		t.Fatal("builtin editable")
	}
	if _, e = CreateSkill("project/code-risk", ""); e == nil {
		t.Fatal("existing skill overwritten")
	}
	if e = os.WriteFile(p, []byte(`{"version":1,"name":"custom","unexpected":true}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSkill(p); e == nil {
		t.Fatal("unknown field accepted")
	}
	if e = os.WriteFile(p, []byte(`{"version":1,"version":2}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadSkill(p); e == nil {
		t.Fatal("duplicate field accepted")
	}
	if _, e = CreateSkill("project/../../escape", ""); e == nil {
		t.Fatal("traversal name accepted")
	}
}

func TestPatternCoverageAndThresholds(t *testing.T) {
	p, e := LoadPattern("builtin/heads")
	if e != nil {
		t.Fatal(e)
	}
	delete(p.Branches, "code")
	if ValidatePattern(p) == nil {
		t.Fatal("incomplete heads accepted")
	}
	p, e = LoadPattern("builtin/funnel")
	if e != nil {
		t.Fatal(e)
	}
	bad := 1.1
	p.Stages[0].Threshold = &bad
	if ValidatePattern(p) == nil {
		t.Fatal("invalid threshold accepted")
	}
}

func TestResolvedSkillPreservesLiteralParameterSyntax(t *testing.T) {
	s, err := LoadSkill("builtin/relevance")
	if err != nil {
		t.Fatal(err)
	}
	s, err = ResolveParameters(s, map[string]string{"question": "literal {{other}}"})
	if err != nil {
		t.Fatal(err)
	}
	s.Resolved = true
	if err := ValidateSkill(s); err != nil {
		t.Fatal(err)
	}
	before := string(s.Questions["relevant"])
	result, err := ResolveParameters(s, nil)
	if err != nil || string(result.Questions["relevant"]) != before {
		t.Fatalf("frozen question changed: %v", err)
	}
	if _, err := ResolveParameters(s, map[string]string{"question": "new"}); err == nil {
		t.Fatal("frozen parameters were overridden")
	}
}
