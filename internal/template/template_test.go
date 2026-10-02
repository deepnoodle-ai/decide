package template

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate gives the test an empty working directory and DECIDE_HOME.
func isolate(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("DECIDE_HOME", t.TempDir())
}

func TestBuiltinsAreValid(t *testing.T) {
	isolate(t)
	all, broken, err := List()
	if err != nil || len(broken) > 0 {
		t.Fatal(err, broken)
	}
	var names []string
	for _, s := range all {
		names = append(names, s.Name)
		if s.Location != BuiltIn || s.Docs == "" {
			t.Errorf("%s: location %q, docs %d bytes", s.Name, s.Location, len(s.Docs))
		}
	}
	if got := strings.Join(names, ","); got != "code-risk,pr-description,prompt-injection,receipt-quality,relevance,sentiment,task-readiness,ticket-routing,triage" {
		t.Fatalf("builtins = %s", got)
	}
}

func TestQuestionOrderIsPreserved(t *testing.T) {
	isolate(t)
	s, err := Load("code-risk")
	if err != nil {
		t.Fatal(err)
	}
	if s.Questions[0].Key != "risk" || s.Questions[1].Key != "maintainability" {
		t.Fatalf("order = %s, %s", s.Questions[0].Key, s.Questions[1].Key)
	}
}

func TestProjectTemplateHidesBuiltin(t *testing.T) {
	isolate(t)
	writeTemplate(t, filepath.Join(ProjectDir, "sentiment"), `{
		"name": "sentiment", "description": "Ours.", "input": "record",
		"questions": {"happy": {"type": "noul", "instructions": "Happy?"}}}`)
	s, err := Load("sentiment")
	if err != nil {
		t.Fatal(err)
	}
	if s.Location != Project || s.Description != "Ours." {
		t.Fatalf("loaded %s template %q", s.Location, s.Description)
	}
	all, _, _ := List()
	count := 0
	for _, s := range all {
		if s.Name == "sentiment" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("List shows sentiment %d times", count)
	}
}

func TestBrokenTemplateDoesNotHideOthers(t *testing.T) {
	isolate(t)
	writeTemplate(t, filepath.Join(UserDir(), "broken"), "{\n  \"name\": \"broken\"\n  \"input\": \"record\"\n}")
	all, broken, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 9 || len(broken) != 1 || !strings.Contains(broken[0].Error(), "template.json:3:3") {
		t.Fatalf("List = %d templates, broken %v", len(all), broken)
	}
}

func TestLoadUnknown(t *testing.T) {
	isolate(t)
	os.Mkdir("folder", 0o755)
	for _, name := range []string{"nope", "README.md", "Bad Name", "folder/", "./folder"} {
		var nf *NotFoundError
		if _, err := Load(name); !errors.As(err, &nf) {
			t.Errorf("Load(%q) = %v, want NotFoundError", name, err)
		}
	}
}

func TestValidateExplainsProblems(t *testing.T) {
	cases := map[string]string{
		`{"name":"x","description":"d","input":"record","questions":{}}`:                                                           "at least one question",
		`{"name":"x","description":"d","input":"rows","questions":{"q":{"type":"noul","instructions":"?"}}}`:                       `input "rows"`,
		`{"name":"x","description":"d","input":"record","questions":{"q":{"type":"noul"}}}`:                                        "needs instructions",
		`{"name":"x","description":"d","input":"record","questions":{"q":{"type":"noul","instructions":"{{topic}}?"}}}`:            "not a declared parameter",
		`{"name":"x","description":"d","input":"record","questions":{"q":{"type":"noul","instructions":"?"}},"extra":true}`:        "unknown field",
		`{"name":"x","description":"d","each":"page","questions":{"q":{"type":"noul","instructions":"?"}}}`:                        `each "page" must be one of file, line, paragraph, section`,
		`{"name":"x","description":"d","input":"image","each":"file","questions":{"q":{"type":"noul","instructions":"?"}}}`:        "has no each",
		`{"name":"x","description":"d","questions":{"q\u001b[2J":{"type":"noul","instructions":"?"}}}`:                             "must not contain control characters",
		`{"name":"x","description":"d","questions":{"q":{"type":"choice","instructions":"?","criteria":{"a\u0007":"A","b":"B"}}}}`: "option",
	}
	for body, want := range cases {
		if _, err := parse([]byte(body), "test"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parse(%s) = %v, want %q", body, err, want)
		}
	}
}

func TestResolveParameters(t *testing.T) {
	isolate(t)
	s, _ := Load("relevance")
	var pe *ParamError
	if _, err := s.Resolve(nil); !errors.As(err, &pe) || pe.Missing != "question" {
		t.Fatalf("missing parameter: %v", err)
	}
	if _, err := s.Resolve(map[string]string{"question": "x", "topic": "y"}); !errors.As(err, &pe) || pe.Missing != "" {
		t.Fatalf("unknown parameter: %v", err)
	}
	r, err := s.Resolve(map[string]string{"question": `Is it "urgent"?`})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(r.Questions[0].Raw); !strings.Contains(got, `relevant to this topic or question: Is it \"urgent\"?"`) {
		t.Fatalf("substituted question = %s", got)
	}
	if _, err := r.Decode(); err != nil {
		t.Fatal(err)
	}

	code, _ := Load("code-risk")
	r, _ = code.Resolve(nil)
	if !strings.Contains(string(r.Questions[0].Raw), "risk authorization flaws, data loss") {
		t.Fatalf("default not applied: %s", r.Questions[0].Raw)
	}
}

func TestCreate(t *testing.T) {
	isolate(t)
	path, err := Create("mine", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(UserDir(), "mine", "template.json"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if s, err := Load("mine"); err != nil || s.Location != User {
		t.Fatalf("Load(mine) = %v, %v", s, err)
	}
	if _, err := Create("mine", nil, false); err == nil {
		t.Fatal("Create overwrote an existing template")
	}

	from, _ := Load("ticket-routing")
	if _, err := Create("my-triage", from, true); err != nil {
		t.Fatal(err)
	}
	s, err := Load("my-triage")
	if err != nil {
		t.Fatal(err)
	}
	if s.Location != Project || s.Docs != from.Docs || string(s.Questions[0].Raw) == "" {
		t.Fatalf("copy = %+v", s)
	}
	data, _ := os.ReadFile(filepath.Join(ProjectDir, "my-triage", "template.json"))
	if strings.Index(string(data), "billing") > strings.Index(string(data), "other") {
		t.Fatal("copy reordered choice options")
	}
}

func writeTemplate(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "template.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInputsFromEarlierVersions(t *testing.T) {
	for in, want := range map[string]string{
		`"input":"file",`:   "text file",
		`"input":"record",`: "text ",
		``:                  "text ",
		`"input":"image",`:  "image ",
	} {
		s, err := parse([]byte(`{"name":"x","description":"d",`+in+`"questions":{"q":{"type":"noul","instructions":"?"}}}`), "test")
		if err != nil {
			t.Fatal(err)
		}
		if got := string(s.Input) + " " + s.Each; got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
