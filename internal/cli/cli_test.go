package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

type harness struct {
	t      *testing.T
	server *decidetest.Server
	dir    string
}

// setup isolates DECIDE_HOME and the working directory, and connects the
// app to a fake TypeSafe server.
func setup(t *testing.T) *harness {
	t.Helper()
	t.Setenv("DECIDE_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	return &harness{t: t, server: decidetest.NewServer(t), dir: dir}
}

type output struct {
	stdout, stderr string
	code           int
}

func (h *harness) run(stdin string, args ...string) output {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	app := &App{
		Stdin:  strings.NewReader(stdin),
		Stdout: &stdout,
		Stderr: &stderr,
		NewClient: func(provider, model string) (*decide.Client, error) {
			return h.server.NewClient(decide.WithModel(model))
		},
	}
	code := app.Run(context.Background(), args)
	return output{stdout.String(), stderr.String(), code}
}

func (h *harness) write(name, body string) {
	h.t.Helper()
	path := filepath.Join(h.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func contains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
}

func TestOverview(t *testing.T) {
	h := setup(t)
	out := h.run("")
	if out.code != 0 {
		t.Fatalf("exit %d", out.code)
	}
	contains(t, out.stdout, "Get started", "decide skills", "TYPESAFE_API_KEY")
	out = h.run("", "run", "--help")
	contains(t, out.stdout, "Examples:", "--dry-run", "--param")
}

func TestSkills(t *testing.T) {
	h := setup(t)
	out := h.run("", "skills")
	contains(t, out.stdout, "code-risk", "sentiment", "ticket-routing", "decide skills show")

	out = h.run("", "skills", "show", "relevance")
	contains(t, out.stdout, "Is this item relevant to: {{question}}?", "required", `decide run relevance data.jsonl -p question="..."`)

	out = h.run("", "skills", "new", "triage", "--from", "ticket-routing")
	if out.code != 0 {
		t.Fatalf("new: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "Created", "triage/skill.json", "decide run triage data.jsonl --dry-run")
	out = h.run("", "skills")
	contains(t, out.stdout, "triage", "(user)")

	out = h.run("", "skills", "show", "code-risk")
	contains(t, out.stdout, "plausible authorization, data loss")

	h.write(".decide/skills/broken/skill.json", `{"name": "broken",}`)
	out = h.run("", "skills")
	contains(t, out.stdout, "sentiment")
	contains(t, out.stderr, "Could not load a skill", "skill.json:1:")

	out = h.run("", "skills", "show", "nope")
	if out.code != 1 {
		t.Fatalf("show nope: exit %d", out.code)
	}
	contains(t, out.stderr, `There is no skill named "nope"`, "decide skills")
}

func TestRunFromStdinAndView(t *testing.T) {
	h := setup(t)
	h.server.Answer("sentiment", decidetest.ChoiceAnswer(map[string]float64{"positive": 0.9, "negative": 0.05, "neutral": 0.05}))
	out := h.run("I love it\nIt broke\n", "run", "sentiment")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "stdin:1", "I love it", "sentiment  positive  90%")
	contains(t, out.stderr, "Running sentiment on 2 records", "✓ 2 answered", "decide runs view")

	out = h.run("", "runs")
	contains(t, out.stdout, "sentiment", "2 answered")

	out = h.run("", "runs", "view", "--details")
	contains(t, out.stdout, "stdin:2", "positive", "negative", "confidence")

	out = h.run("", "runs", "view", "--json")
	lines := strings.Split(strings.TrimSpace(out.stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d JSON lines", len(lines))
	}
	var res struct {
		Source  string                     `json:"source"`
		Status  string                     `json:"status"`
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &res); err != nil {
		t.Fatal(err)
	}
	if res.Source != "stdin:1" || res.Status != "complete" || res.Answers["sentiment"] == nil {
		t.Fatalf("result = %+v", res)
	}
}

func TestRunFilesWithParameters(t *testing.T) {
	h := setup(t)
	h.write("src/main.go", "package main")
	h.write("src/main_test.go", "package main")
	out := h.run("", "run", "code-risk", "src", "--exclude", "*_test.go", "-p", "focus=SQL injection", "--json")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	reqs := h.server.Requests()
	if len(reqs) != 1 {
		t.Fatalf("sent %d requests", len(reqs))
	}
	contains(t, string(reqs[0].Body), "plausible SQL injection risk", `"path":"src/main.go"`)
}

func TestDryRunMakesNoCalls(t *testing.T) {
	h := setup(t)
	h.write("tickets.jsonl", `{"id":1,"body":"Refund please"}`+"\n"+`{"id":2,"body":"App crashes"}`+"\n")
	out := h.run("", "run", "ticket-routing", "tickets.jsonl", "--field", "body", "--dry-run")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "ticket-routing would look at 2 records", "tickets.jsonl:2", "queue", "one of billing, engineering, other", "Nothing was sent")
	if n := len(h.server.Requests()); n != 0 {
		t.Fatalf("dry run sent %d requests", n)
	}
	if out := h.run("", "runs"); !strings.Contains(out.stdout, "No runs yet") {
		t.Fatalf("dry run saved a run:\n%s", out.stdout)
	}
}

func TestFailuresAndResume(t *testing.T) {
	h := setup(t)
	h.server.FailNext(422)
	out := h.run("one\ntwo\n", "run", "sentiment", "--workers", "1")
	if out.code != 1 {
		t.Fatalf("exit %d", out.code)
	}
	contains(t, out.stdout, "✗")
	contains(t, out.stderr, "✓ 1 answered", "✗ 1 failed", "Retry the failed items with: decide runs resume")

	out = h.run("", "runs", "resume")
	if out.code != 0 {
		t.Fatalf("resume: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Resuming run", "1 record left", "✓ 2 answered")
	out = h.run("", "runs", "resume")
	contains(t, out.stdout, "already has an answer")
}

func TestPipedTextThatLooksLikeJSON(t *testing.T) {
	h := setup(t)
	out := h.run("[INFO] started\n[WARN] disk full\n", "run", "sentiment", "--dry-run")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "2 records", "[WARN] disk full")
}

func TestHelpfulErrors(t *testing.T) {
	h := setup(t)
	h.write("notes.txt", "hello")
	for _, tc := range []struct {
		name  string
		args  []string
		wants []string
	}{
		{"no skill", []string{"run"}, []string{"Which skill", "decide skills"}},
		{"data first", []string{"run", "notes.txt"}, []string{`no skill named "notes.txt"`, "decide run SKILL notes.txt"}},
		{"folder first", []string{"run", "./"}, []string{`no skill named "./"`, "decide run SKILL ./"}},
		{"missing parameter", []string{"run", "relevance", "notes.txt"}, []string{`needs a value for "question"`, `--param question="..."`}},
		{"unknown parameter", []string{"run", "sentiment", "notes.txt", "-p", "x=1"}, []string{"no parameter", "has no parameters"}},
		{"bad parameter", []string{"run", "relevance", "notes.txt", "-p", "question"}, []string{"name=value"}},
		{"limit and sample", []string{"run", "sentiment", "notes.txt", "-n", "1", "--sample", "2"}, []string{"--limit or --sample"}},
		{"images need cloudflare", []string{"run", "receipt-quality", ".", "--provider", "typesafe"}, []string{"--provider cloudflare"}},
		{"nothing matched", []string{"run", "code-risk", ".", "--include", "*.rs"}, []string{"Found nothing", "--include"}},
		{"bad provider", []string{"run", "sentiment", "--provider", "openai"}, []string{"openai"}},
		{"no runs", []string{"runs", "view"}, []string{"no saved runs"}},
		{"unknown run", []string{"runs", "view", "nope"}, []string{`There is no run "nope"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := h.run("", tc.args...)
			if out.code != 1 {
				t.Errorf("exit %d, want 1", out.code)
			}
			contains(t, out.stderr, tc.wants...)
		})
	}
}

func TestMissingCredentials(t *testing.T) {
	setup(t)
	t.Setenv("TYPESAFE_API_KEY", "")
	var stderr bytes.Buffer
	app := &App{Stdin: strings.NewReader("hi"), Stdout: &bytes.Buffer{}, Stderr: &stderr}
	if code := app.Run(context.Background(), []string{"run", "sentiment"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	contains(t, stderr.String(), "TYPESAFE_API_KEY is not set", "export TYPESAFE_API_KEY", "--dry-run")
}
