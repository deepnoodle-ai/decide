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
	"github.com/deepnoodle-ai/decide/internal/source"
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
	contains(t, out.stdout, "Get started", "decide templates", "TYPESAFE_API_KEY")
	out = h.run("", "run", "--help")
	contains(t, out.stdout, "Examples:", "--dry-run", "--param")
}

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	app := &App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stdout, Version: "v1.2.3"}
	if code := app.Run(context.Background(), []string{"--version"}); code != 0 || strings.TrimSpace(stdout.String()) != "v1.2.3" {
		t.Fatalf("exit %d, output %q", code, stdout.String())
	}
}

func TestTemplates(t *testing.T) {
	h := setup(t)
	out := h.run("", "templates")
	contains(t, out.stdout, "code-risk", "sentiment", "ticket-routing", "decide templates show")

	out = h.run("", "templates", "show", "relevance")
	contains(t, out.stdout, "Is this item relevant to this topic or question: {{question}}", "matches when yes is 60% or more likely", "required", `decide run relevance data.jsonl -p question="..."`)

	out = h.run("", "templates", "new", "my-triage", "--from", "ticket-routing")
	if out.code != 0 {
		t.Fatalf("new: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "Created", "my-triage/template.json", "decide run my-triage data.jsonl --dry-run")
	out = h.run("", "templates")
	contains(t, out.stdout, "my-triage", "(user)")

	out = h.run("", "templates", "show", "code-risk")
	contains(t, out.stdout, "Does this code or configuration", "flagged when", "the score is 1.5 or lower", "yes is 60% or more likely")

	h.write(".decide/templates/broken/template.json", `{"name": "broken",}`)
	out = h.run("", "templates")
	contains(t, out.stdout, "sentiment")
	contains(t, out.stderr, "Could not load a template", "template.json:1:")

	out = h.run("", "templates", "show", "nope")
	if out.code != 1 {
		t.Fatalf("show nope: exit %d", out.code)
	}
	contains(t, out.stderr, `There is no template named "nope"`, "decide templates")
}

func TestRunFromStdinAndView(t *testing.T) {
	h := setup(t)
	h.server.Answer("sentiment", decidetest.ChoiceAnswer(map[string]float64{"positive": 0.9, "negative": 0.05, "neutral": 0.05}))
	out := h.run("I love it\nIt broke\n", "run", "sentiment")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "stdin:1", "I love it", "sentiment  positive  90%")
	contains(t, out.stderr, "Running sentiment on 2 lines", "✓ 2 answered", "decide runs view")

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
	contains(t, string(reqs[0].Body), "risk SQL injection?", `"path":"src/main.go"`)
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
	contains(t, out.stderr, "Resuming run", "1 request left", "✓ 2 answered")
	out = h.run("", "runs", "resume")
	contains(t, out.stderr, "already has an answer")
}

func TestPipedTextThatLooksLikeJSON(t *testing.T) {
	h := setup(t)
	out := h.run("[INFO] started\n[WARN] disk full\n", "run", "sentiment", "--dry-run")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "2 lines", "[WARN] disk full")
}

func TestHelpfulErrors(t *testing.T) {
	h := setup(t)
	h.write("notes.txt", "hello")
	for _, tc := range []struct {
		name  string
		args  []string
		wants []string
	}{
		{"no template", []string{"run"}, []string{"Which template", "decide templates"}},
		{"data first", []string{"run", "notes.txt"}, []string{`no template named "notes.txt"`, "decide run TEMPLATE notes.txt"}},
		{"folder first", []string{"run", "./"}, []string{`no template named "./"`, "decide run TEMPLATE ./"}},
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

func TestFlaggedAnswers(t *testing.T) {
	h := setup(t)
	h.write("src/a.go", "package a")
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Answer("risk", decidetest.NoulAnswer(0.88))
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0.1, 0.5, 0.4))
	out := h.run("", "run", "code-risk", "src")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	// Answers and figures line up in columns across questions.
	contains(t, out.stdout,
		"! risk             yes           88%\n",
		"  maintainability  ━━━━━━━━━━──  3.3 of 4  3\n")
	contains(t, out.stderr, "! 1 flagged", "Flagged: src/a.go")

	h.server.Answer("risk", decidetest.NoulAnswer(0.5))
	out = h.run("", "run", "code-risk", "src", "--no-cache")
	contains(t, out.stdout, "  risk             unsure        50% yes\n")
	contains(t, out.stderr, "nothing flagged")

	h.server.Answer("risk", decidetest.NoulAnswer(0.1))
	out = h.run("", "run", "code-risk", "src", "--no-cache")
	contains(t, out.stdout, "  risk             no            90%\n")

	// Templates without flags do not mention them.
	out = h.run("ok\n", "run", "relevance", "-p", "question=x")
	if strings.Contains(out.stderr, "flagged") {
		t.Fatalf("relevance has no flags:\n%s", out.stderr)
	}
}

func TestFailOn(t *testing.T) {
	h := setup(t)
	h.write("src/a.go", "package a")
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0.1, 0.5, 0.4))

	h.server.Answer("risk", decidetest.NoulAnswer(0.88))
	out := h.run("", "run", "code-risk", "src", "--fail-on", "flagged")
	if out.code != 2 {
		t.Fatalf("flagged: exit %d, want 2: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "! 1 flagged", "Exiting with code 2 because 1 item was flagged")

	// Without --fail-on, flagged items still exit 0.
	out = h.run("", "run", "code-risk", "src")
	if out.code != 0 {
		t.Fatalf("without --fail-on: exit %d, want 0", out.code)
	}

	// An answer close to flagged is not flagged.
	h.server.Answer("risk", decidetest.NoulAnswer(0.5))
	out = h.run("", "run", "code-risk", "src", "--fail-on", "flagged", "--no-cache")
	if out.code != 0 {
		t.Fatalf("unsure: exit %d, want 0: %s", out.code, out.stderr)
	}

	// Matches fail the same way.
	h.server.Answer("relevant", decidetest.NoulAnswer(0.9))
	out = h.run("ok\n", "run", "relevance", "-p", "question=x", "--fail-on", "matched")
	if out.code != 2 {
		t.Fatalf("matched: exit %d, want 2: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Exiting with code 2 because 1 item was matched")

	// A failed item wins over a flagged one: the run is not finished.
	h.server.Answer("risk", decidetest.NoulAnswer(0.88))
	h.write("src/b.go", "package b")
	h.server.FailNext(422)
	out = h.run("", "run", "code-risk", "src", "--fail-on", "flagged", "--workers", "1", "--no-cache")
	if out.code != 1 {
		t.Fatalf("partial: exit %d, want 1: %s", out.code, out.stderr)
	}
	// Resuming with --fail-on finishes the run and then gates on it.
	out = h.run("", "runs", "resume", "--fail-on", "flagged")
	if out.code != 2 {
		t.Fatalf("resume: exit %d, want 2: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Exiting with code 2 because 2 items were flagged")
	out = h.run("", "runs", "resume", "--fail-on", "flagged")
	if out.code != 2 {
		t.Fatalf("resume of a finished run: exit %d, want 2: %s", out.code, out.stderr)
	}

	// A template that never flags could never fail.
	out = h.run("ok\n", "run", "relevance", "-p", "question=x", "--fail-on", "flagged")
	if out.code != 1 {
		t.Fatalf("relevance: exit %d, want 1", out.code)
	}
	contains(t, out.stderr, "relevance never marks an item flagged")
}

func TestDiffInput(t *testing.T) {
	h := setup(t)
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@ func A() {\n a\n-b\n+c\n@@ -9 +9 @@\n-x\n+y\n" +
		"diff --git a/go.sum b/go.sum\n--- a/go.sum\n+++ b/go.sum\n@@ -1 +1 @@\n-a\n+b\n"
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Answer("risk", decidetest.NoulAnswer(0.9))
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0.1, 0.5, 0.4))
	out := h.run(diff, "run", "code-risk", "--each", "hunk", "--fail-on", "flagged")
	if out.code != 2 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stderr, "Skipped go.sum (lockfile)", "Running code-risk on 2 hunks", "Flagged: a.go:2, a.go:9")
	contains(t, out.stdout, "a.go:2  func A() {\n")

	// Without a unit, the hint points out --each function.
	out = h.run(diff, "run", "sentiment")
	contains(t, out.stderr, "Running sentiment on 2 hunks", "add --each function")
	out = h.run("diff --git a/R.md b/R.md\n--- a/R.md\n+++ b/R.md\n@@ -1 +1 @@\n-a\n+b\n", "run", "sentiment")
	if strings.Contains(out.stderr, "--each function") {
		t.Errorf("a diff without code suggests --each function:\n%s", out.stderr)
	}

	// code-risk reads whole files, so a diff is judged by changed file.
	out = h.run(diff, "run", "code-risk")
	contains(t, out.stderr, "Running code-risk on 1 file")
	if strings.Contains(out.stderr, "--each function") {
		t.Errorf("a changed file is not a source file to split:\n%s", out.stderr)
	}
}

func TestNothingToJudge(t *testing.T) {
	h := setup(t)
	lockOnly := "diff --git a/go.sum b/go.sum\n--- a/go.sum\n+++ b/go.sum\n@@ -1 +1 @@\n-a\n+b\n"
	for name, stdin := range map[string]string{"empty diff": "", "skipped files": lockOnly} {
		for _, extra := range [][]string{nil, {"--dry-run"}} {
			args := append([]string{"run", "code-risk", "--fail-on", "flagged"}, extra...)
			out := h.run(stdin, args...)
			if out.code != 0 {
				t.Errorf("%s %v: exit %d, want 0: %s", name, extra, out.code, out.stderr)
			}
			contains(t, out.stderr, "Nothing for code-risk to judge")
		}
	}
	if len(h.server.Requests()) != 0 {
		t.Errorf("sent %d requests", len(h.server.Requests()))
	}
	// A change with no code has no functions to judge.
	docs := "diff --git a/R.md b/R.md\n--- a/R.md\n+++ b/R.md\n@@ -1 +1 @@\n-a\n+b\n"
	if out := h.run(docs, "run", "code-risk", "--each", "function", "--fail-on", "flagged"); out.code != 0 {
		t.Errorf("docs only: exit %d, want 0: %s", out.code, out.stderr)
	}

	// A folder with nothing in it is still a mistake worth reporting.
	h.write("empty/.keep", "")
	if out := h.run("", "run", "code-risk", "empty"); out.code != 1 {
		t.Errorf("empty folder: exit %d, want 1", out.code)
	}
}

func TestMatchedAnswers(t *testing.T) {
	h := setup(t)
	h.write("notes.txt", "about tools\nabout pricing\n")
	h.server.Answer("relevant", decidetest.NoulAnswer(0.9))
	out := h.run("", "run", "relevance", "notes.txt", "-p", "question=tools")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "notes.txt:1  about tools\n● relevant  yes     90%\n")
	contains(t, out.stderr, "● 2 matched", "Matched: notes.txt:1, notes.txt:2")

	h.server.Answer("relevant", decidetest.NoulAnswer(0.1))
	out = h.run("", "run", "relevance", "notes.txt", "-p", "question=tools", "--no-cache")
	contains(t, out.stdout, "  relevant  no      90%\n")
	contains(t, out.stderr, "no matches")
	if strings.Contains(out.stderr, "Matched:") {
		t.Fatalf("listed matches without any:\n%s", out.stderr)
	}
}

// partsServer answers code-risk questions by part: the part holding
// "risky" is risky, and every part scores 3 of 4.
func partsServer(h *harness) {
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Respond(func(req *decide.Request) (*decide.Response, error) {
		state, _ := json.Marshal(req.State)
		risk := 0.1
		if strings.Contains(string(state), "risky") {
			risk = 0.9
		}
		return &decide.Response{Answers: map[string]decide.Answer{
			"risk":            decidetest.NoulAnswer(risk),
			"maintainability": decidetest.ScoreAnswer(levels, 0, 0, 0, 1, 0),
		}}, nil
	})
}

func TestLargeFilesAreJudgedInParts(t *testing.T) {
	h := setup(t)
	defer func(n int) { source.MaxItemBytes = n }(source.MaxItemBytes)
	source.MaxItemBytes = source.StateRoom + len("src/a.go") + 20
	h.write("src/a.go", "package a\n\nfunc risky() {}\n\nfunc fine() {}\n")
	partsServer(h)

	out := h.run("", "run", "code-risk", "src", "--dry-run")
	contains(t, out.stdout, "1 file (1 judged in parts, 3 requests)", "src/a.go  in 3 parts")

	out = h.run("", "run", "code-risk", "src")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	// One answer for the file: flagged, because one part is.
	contains(t, out.stdout, "src/a.go  judged in 3 parts\n", "! risk             yes           90%  lines 3\n")
	contains(t, out.stderr, "Running code-risk on 1 file (1 judged in parts, 3 requests)", "✓ 1 answered  ! 1 flagged", "Flagged: src/a.go")
	if strings.Count(out.stdout, "src/a.go") != 1 {
		t.Fatalf("the parts printed separately:\n%s", out.stdout)
	}
	out = h.run("", "runs", "view")
	contains(t, out.stdout, "src/a.go  judged in 3 parts\n", "lines 3")
}

func TestResumeCombinesNewAndSavedParts(t *testing.T) {
	h := setup(t)
	defer func(n int) { source.MaxItemBytes = n }(source.MaxItemBytes)
	source.MaxItemBytes = source.StateRoom + len("src/a.go") + 20
	h.write("src/a.go", "package a\n\nfunc risky() {}\n\nfunc fine() {}\n")
	partsServer(h)
	h.server.FailNext(422)
	out := h.run("", "run", "code-risk", "src", "--workers", "1")
	if out.code != 1 {
		t.Fatalf("exit %d", out.code)
	}
	contains(t, out.stdout, "✗ part 1 of 3 (lines 1)")
	contains(t, out.stderr, "✗ 1 failed")

	out = h.run("", "runs", "resume")
	if out.code != 0 {
		t.Fatalf("resume: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "src/a.go  judged in 3 parts\n", "! risk             yes           90%  lines 3\n")
	contains(t, out.stderr, "1 request left", "✓ 1 answered  ! 1 flagged")
}

func TestEachHintsAndErrors(t *testing.T) {
	h := setup(t)
	h.write("docs/guide.md", "# Guide\n\n## Install\n\nRun it.\n")
	out := h.run("", "run", "relevance", "docs", "-p", "question=x", "--dry-run")
	contains(t, out.stdout, "would look at 1 file:", "add --each section or --each paragraph")
	out = h.run("", "run", "relevance", "docs", "-p", "question=x", "--each", "section", "--dry-run")
	contains(t, out.stdout, "would look at 1 section:", "docs/guide.md#install  Guide › Install")
	if strings.Contains(out.stdout, "add --each") {
		t.Fatalf("hinted at --each after it was given:\n%s", out.stdout)
	}
	out = h.run("", "run", "receipt-quality", "docs", "--each", "file")
	if out.code == 0 || !strings.Contains(out.stderr, "--each does not apply") {
		t.Fatalf("image template with --each: %d %s", out.code, out.stderr)
	}
}

func TestEachFunction(t *testing.T) {
	h := setup(t)
	h.write("src/store.py", "class Store:\n    def save(self):\n        pass\n\n    def load(self):\n        pass\n")
	h.write("src/notes.md", "# Notes\n")
	out := h.run("", "run", "code-risk", "src", "--dry-run")
	contains(t, out.stdout, "would look at 2 files:", "To judge each function instead, add --each function.")
	out = h.run("", "run", "code-risk", "src", "--each", "function", "--dry-run")
	contains(t, out.stdout, "would look at 2 functions:", "src/store.py#L2  Store.save", "src/store.py#L5  Store.load")
	contains(t, out.stderr, "Skipped 1 file not in Go, Python, JavaScript, TypeScript, or Java")
	out = h.run("", "run", "code-risk", "src/notes.md", "--each", "function")
	if out.code == 0 || !strings.Contains(out.stderr, "use --each file") {
		t.Fatalf("Markdown with --each function: %d %s", out.code, out.stderr)
	}
}

func TestPartsCountAsOneItem(t *testing.T) {
	h := setup(t)
	defer func(n int) { source.MaxItemBytes = n }(source.MaxItemBytes)
	source.MaxItemBytes = source.StateRoom + len("src/a.go") + 20
	h.write("src/a.go", "package a\n\nfunc risky() {}\n\nfunc fine() {}\n")
	partsServer(h)

	// --json prints one line per item.
	out := h.run("", "run", "code-risk", "src", "--json")
	if strings.Count(out.stdout, "\n") != 1 {
		t.Fatalf("--json printed %d lines:\n%s", strings.Count(out.stdout, "\n"), out.stdout)
	}
	var it struct {
		Source string
		Parts  int
		Where  map[string]string
		Part   any
	}
	if err := json.Unmarshal([]byte(out.stdout), &it); err != nil {
		t.Fatal(err)
	}
	if it.Source != "src/a.go" || it.Parts != 3 || it.Where["risk"] != "lines 3" || it.Part != nil {
		t.Fatalf("item = %+v", it)
	}
	out = h.run("", "runs", "view", "--json")
	if strings.Count(out.stdout, "\n") != 1 {
		t.Fatalf("runs view --json printed:\n%s", out.stdout)
	}
	// runs list counts items, as the run's summary does.
	out = h.run("", "runs")
	contains(t, out.stdout, "1 answered")
}

func TestFailedPartsDoNotStopTheRun(t *testing.T) {
	h := setup(t)
	defer func(n int) { source.MaxItemBytes = n }(source.MaxItemBytes)
	source.MaxItemBytes = source.StateRoom + len("src/a.go") + 20
	h.write("src/a.go", strings.Repeat("func f() {}\n\n", 6))
	h.write("src/b.go", "package b\n")
	for range 6 {
		h.server.FailNext(422)
	}
	out := h.run("", "run", "code-risk", "src", "--workers", "1")
	if out.code != 1 || strings.Contains(out.stderr, "Stopped because") {
		t.Fatalf("exit %d:\n%s", out.code, out.stderr)
	}
	contains(t, out.stdout, "✗ part 1 of 6 (lines 1)", "src/b.go")
	contains(t, out.stderr, "✓ 1 answered  ✗ 1 failed")
}

func TestLargeRecordsAreJudgedInParts(t *testing.T) {
	h := setup(t)
	defer func(n int) { source.MaxItemBytes = n }(source.MaxItemBytes)
	source.MaxItemBytes = source.StateRoom + len("t.jsonl:1") + 40
	h.write("t.jsonl", `{"body":"hay hay hay\nhay hay hay\nthe needle\nhay hay hay"}`+"\n"+`{"body":"hay"}`+"\n")
	h.server.Respond(func(req *decide.Request) (*decide.Response, error) {
		state, _ := json.Marshal(req.State)
		p := 0.1
		if strings.Contains(string(state), "needle") {
			p = 0.9
		}
		return &decide.Response{Answers: map[string]decide.Answer{"relevant": decidetest.NoulAnswer(p)}}, nil
	})
	h.server.FailNext(422)
	out := h.run("", "run", "relevance", "t.jsonl", "--field", "body", "-p", "question=needles", "--workers", "1")
	contains(t, out.stdout, "✗ part 1 of 2: ")
	contains(t, out.stderr, "Running relevance on 2 records (1 judged in parts, 3 requests)", "✓ 1 answered  ✗ 1 failed")

	// Resuming prints the item answered before, then the one it finishes.
	out = h.run("", "runs", "resume", "--json")
	lines := strings.Split(strings.TrimSpace(out.stdout), "\n")
	if out.code != 0 || len(lines) != 2 {
		t.Fatalf("resume: exit %d:\n%s%s", out.code, out.stdout, out.stderr)
	}
	contains(t, lines[0], `"source":"t.jsonl:2"`)
	var it struct {
		Source string
		Parts  int
		Where  map[string]string
	}
	json.Unmarshal([]byte(lines[1]), &it)
	if it.Source != "t.jsonl:1" || it.Parts != 2 || it.Where["relevant"] != "part 1 of 2" {
		t.Fatalf("item = %+v", it)
	}
	out = h.run("", "runs", "view")
	contains(t, out.stdout, "t.jsonl:1  {\"body\":\"hay hay hay\\nhay", "judged in 2 parts\n● relevant  yes     90%  part 1 of 2\n")
	contains(t, out.stderr, "✓ 2 answered  ● 1 matched")
}

func TestTemplateAndFileTextCannotControlTheTerminal(t *testing.T) {
	h := setup(t)
	h.write(".decide/templates/odd/template.json", `{
  "name": "odd",
  "description": "Odd \u001b[31mtemplate",
  "parameters": {"p": {"description": "A \u001b[2J parameter", "default": "x\u001b[0m"}},
  "questions": {
    "tone": {
      "type": "choice",
      "instructions": "Pick one {{p}} \u001b]0;title\u0007",
      "criteria": {"calm": "Calm \u001b[1m", "angry": "Angry"}
    },
    "level": {"type": "score", "instructions": "Rate it", "criteria": ["low \u001b[31m", "high"]}
  },
  "flags": {"tone": "calm >= 60%"}
}`)
	h.write(".decide/templates/odd/README.md", "Notes \x1b[31mhere\n")
	h.write("in/a\x1b[31m.txt", "hello")
	h.write("in/b\x1b[31m.bin", "\x00\x01\x02binary")
	for _, args := range [][]string{
		{"templates"},
		{"templates", "show", "odd"},
		{"run", "odd", "in", "--dry-run"},
	} {
		out := h.run("", args...)
		if out.code != 0 {
			t.Fatalf("%v: exit %d: %s", args, out.code, out.stderr)
		}
		if strings.ContainsAny(out.stdout+out.stderr, "\x1b\x07") {
			t.Errorf("%v printed a control character:\n%q\n%q", args, out.stdout, out.stderr)
		}
	}

	h.write(".decide/templates/bad/template.json", `{"name": "bad", "description": "Bad",
  "questions": {"q\u001b[31m": {"type": "noul", "instructions": "?"}}}`)
	h.write(".decide/templates/broken\x1b[31m/template.json", `{`)
	h.write("bad/x\x1b]0;title\x07.jsonl", "{\"a\":1}\nnot json\n")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"templates"}, "control characters"},
		{[]string{"templates", "show", "bad"}, "control characters"},
		{[]string{"templates"}, "broken"},
		{[]string{"run", "odd", "bad", "--dry-run"}, "not valid JSON"},
	} {
		out := h.run("", c.args...)
		if !strings.Contains(out.stderr, c.want) {
			t.Errorf("%v: stderr %q, want %q", c.args, out.stderr, c.want)
		}
		if strings.ContainsAny(out.stdout+out.stderr, "\x1b\x07") {
			t.Errorf("%v printed a control character:\n%q\n%q", c.args, out.stdout, out.stderr)
		}
	}
}

// TestRunsViewTop checks that --top shows the items likeliest to be
// flagged, the likeliest first, across a template's flagged questions.
func TestRunsViewTop(t *testing.T) {
	h := setup(t)
	h.server.Respond(func(req *decide.Request) (*decide.Response, error) {
		state, _ := json.Marshal(req.State)
		injection, hidden := 0.1, 0.1
		switch {
		case strings.Contains(string(state), "steer"):
			injection = 0.7
		case strings.Contains(string(state), "hide"):
			hidden = 0.9
		case strings.Contains(string(state), "maybe"):
			injection = 0.45
		}
		return &decide.Response{Answers: map[string]decide.Answer{
			"injection": decidetest.NoulAnswer(injection),
			"hidden":    decidetest.NoulAnswer(hidden),
		}}, nil
	})
	out := h.run("plain\nmaybe\nsteer\nhide\n", "run", "prompt-injection")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}

	out = h.run("", "runs", "view", "--top", "3", "--json")
	var sources []string
	for _, line := range strings.Split(strings.TrimSpace(out.stdout), "\n") {
		var res struct{ Source string }
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, res.Source)
	}
	if got := strings.Join(sources, ","); got != "stdin:4,stdin:3,stdin:2" {
		t.Fatalf("top 3 = %s", got)
	}

	out = h.run("", "runs", "view", "--top", "1")
	contains(t, out.stdout, "stdin:4")
	contains(t, out.stderr, "The 1 of 4 items likeliest to be flagged")
	if strings.Contains(out.stdout, "stdin:3") {
		t.Fatalf("--top 1 printed more than one item:\n%s", out.stdout)
	}

	out = h.run("", "runs", "view", "--top", "2", "--format", "md")
	if out.code != 1 || !strings.Contains(out.stderr, "not md") {
		t.Fatalf("--top with md: exit %d: %s", out.code, out.stderr)
	}
	out = h.run("", "runs", "view", "--top", "-1")
	if out.code != 1 || !strings.Contains(out.stderr, "at least 1") {
		t.Fatalf("--top -1: exit %d: %s", out.code, out.stderr)
	}

	h.write(".decide/templates/plain/template.json", `{"name": "plain", "description": "No flags.",
		"questions": {"long": {"type": "noul", "instructions": "Is it long?"}}}`)
	h.server.Reset()
	h.run("a\n", "run", "plain")
	out = h.run("", "runs", "view", "--top", "5")
	if out.code != 1 || !strings.Contains(out.stderr, "plain flags none") {
		t.Fatalf("--top without flags: exit %d: %s", out.code, out.stderr)
	}
}
