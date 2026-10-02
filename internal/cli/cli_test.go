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
	contains(t, out.stdout, "Get started", "decide skills", "TYPESAFE_API_KEY")
	out = h.run("", "run", "--help")
	contains(t, out.stdout, "Examples:", "--dry-run", "--param")
}

func TestSkills(t *testing.T) {
	h := setup(t)
	out := h.run("", "skills")
	contains(t, out.stdout, "code-risk", "sentiment", "ticket-routing", "decide skills show")

	out = h.run("", "skills", "show", "relevance")
	contains(t, out.stdout, "Is this item relevant to this topic or question: {{question}}", "matches when yes is 60% or more likely", "required", `decide run relevance data.jsonl -p question="..."`)

	out = h.run("", "skills", "new", "triage", "--from", "ticket-routing")
	if out.code != 0 {
		t.Fatalf("new: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "Created", "triage/skill.json", "decide run triage data.jsonl --dry-run")
	out = h.run("", "skills")
	contains(t, out.stdout, "triage", "(user)")

	out = h.run("", "skills", "show", "code-risk")
	contains(t, out.stdout, "Does this code or configuration", "flagged when", "the score is 1.5 or lower", "yes is 60% or more likely")

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
	contains(t, out.stdout, "already has an answer")
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
	out = h.run("", "run", "code-risk", "src")
	contains(t, out.stdout, "  risk             unsure        50% yes\n")
	contains(t, out.stderr, "nothing flagged")

	h.server.Answer("risk", decidetest.NoulAnswer(0.1))
	out = h.run("", "run", "code-risk", "src")
	contains(t, out.stdout, "  risk             no            90%\n")

	// Skills without flags do not mention them.
	out = h.run("ok\n", "run", "relevance", "-p", "question=x")
	if strings.Contains(out.stderr, "flagged") {
		t.Fatalf("relevance has no flags:\n%s", out.stderr)
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
	out = h.run("", "run", "relevance", "notes.txt", "-p", "question=tools")
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
		t.Fatalf("image skill with --each: %d %s", out.code, out.stderr)
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

	out = h.run("", "runs", "resume", "--json")
	if out.code != 0 || strings.Count(out.stdout, "\n") != 1 {
		t.Fatalf("resume: exit %d:\n%s%s", out.code, out.stdout, out.stderr)
	}
	var it struct {
		Source string
		Parts  int
		Where  map[string]string
	}
	json.Unmarshal([]byte(out.stdout), &it)
	if it.Source != "t.jsonl:1" || it.Parts != 2 || it.Where["relevant"] != "part 1 of 2" {
		t.Fatalf("item = %+v", it)
	}
	out = h.run("", "runs", "view")
	contains(t, out.stdout, "t.jsonl:1  {\"body\":\"hay hay hay\\nhay", "judged in 2 parts\n● relevant  yes     90%  part 1 of 2\n")
	contains(t, out.stderr, "✓ 2 answered  ● 1 matched")
}

func TestSkillAndFileTextCannotControlTheTerminal(t *testing.T) {
	h := setup(t)
	h.write(".decide/skills/odd/skill.json", `{
  "name": "odd",
  "description": "Odd \u001b[31mskill",
  "parameters": {"p": {"description": "A \u001b[2J parameter", "default": "x\u001b[0m"}},
  "questions": {
    "tone\u001b[31m": {
      "type": "choice",
      "instructions": "Pick one {{p}} \u001b]0;title\u0007",
      "criteria": {"calm\u001b[31m": "Calm \u001b[1m", "angry": "Angry"}
    },
    "level": {"type": "score", "instructions": "Rate it", "criteria": ["low \u001b[31m", "high"]}
  },
  "flags": {"tone\u001b[31m": "calm\u001b[31m >= 60%"}
}`)
	h.write(".decide/skills/odd/SKILL.md", "Notes \x1b[31mhere\n")
	h.write("in/a\x1b[31m.txt", "hello")
	h.write("in/b\x1b[31m.bin", "\x00\x01\x02binary")
	for _, args := range [][]string{
		{"skills"},
		{"skills", "show", "odd"},
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
}
