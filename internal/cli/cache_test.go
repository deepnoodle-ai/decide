package cli

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/source"
	"github.com/deepnoodle-ai/decide/internal/template"
)

// codeRisk answers code-risk and test template questions, flagging files
// that hold "risky", and names a model version.
func codeRisk(h *harness, model string) {
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Respond(func(req *decide.Request) (*decide.Response, error) {
		state, _ := json.Marshal(req.State)
		risk := 0.1
		if strings.Contains(string(state), "risky") {
			risk = 0.9
		}
		all := map[string]decide.Answer{
			"risk":            decidetest.NoulAnswer(risk),
			"maintainability": decidetest.ScoreAnswer(levels, 0, 0, 0, 1, 0),
			"relevant":        decidetest.NoulAnswer(0.8),
			"tested":          decidetest.NoulAnswer(0.3),
		}
		resp := &decide.Response{Model: model, Answers: map[string]decide.Answer{}}
		for key := range req.Questions {
			resp.Answers[key] = all[key]
		}
		return resp, nil
	})
}

func (h *harness) requests() int { return len(h.server.Requests()) }

// jsonLines decodes --json output, with or without the fields that say
// where answers came from.
func jsonLines(t *testing.T, out string, keepSource bool) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(out) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		if !keepSource {
			delete(m, "cached")
			delete(m, "request_id")
			delete(m, "model")
		}
		lines = append(lines, m)
	}
	return lines
}

func TestCacheRerun(t *testing.T) {
	h := setup(t)
	h.write("src/a.go", "package a\n\nfunc risky() {}\n")
	h.write("src/b.go", "package b\n")
	codeRisk(h, "jev-1.13.0")

	first := h.run("", "run", "code-risk", "src", "--json", "--fail-on", "flagged")
	if first.code != 2 || h.requests() != 2 {
		t.Fatalf("exit %d, %d requests: %s", first.code, h.requests(), first.stderr)
	}
	if strings.Contains(first.stderr, "from cache") {
		t.Errorf("a first run reports the cache:\n%s", first.stderr)
	}
	for _, line := range jsonLines(t, first.stdout, true) {
		if line["cached"] != nil || line["request_id"] == "" || line["model"] != "jev-1.13.0" {
			t.Errorf("first run result: %v", line)
		}
	}

	// A second run over the same files asks nothing, and its results,
	// flags and exit code are the same.
	second := h.run("", "run", "code-risk", "src", "--json", "--fail-on", "flagged")
	if second.code != 2 || h.requests() != 2 {
		t.Fatalf("exit %d, %d requests: %s", second.code, h.requests(), second.stderr)
	}
	contains(t, second.stderr, "✓ 2 answered  ! 1 flagged", "  4 answers from cache · 0 asked")
	for _, line := range jsonLines(t, second.stdout, true) {
		if !slices.Equal(line["cached"].([]any), []any{"maintainability", "risk"}) || line["request_id"] != nil || line["model"] != nil {
			t.Errorf("second run result: %v", line)
		}
	}
	a, b := jsonLines(t, first.stdout, false), jsonLines(t, second.stdout, false)
	for i := range a {
		x, _ := json.Marshal(a[i])
		y, _ := json.Marshal(b[i])
		if string(x) != string(y) {
			t.Errorf("results differ:\n%s\n%s", x, y)
		}
	}
	for _, format := range []string{"text", "csv", "github"} {
		asked := h.run("", "run", "code-risk", "src", "--format", format, "--no-cache")
		cached := h.run("", "run", "code-risk", "src", "--format", format)
		if asked.stdout != cached.stdout {
			t.Errorf("--format %s differs:\n%s\n%s", format, asked.stdout, cached.stdout)
		}
	}

	// A changed file is asked again; the other comes from the cache.
	n := h.requests()
	h.write("src/b.go", "package b\n\nfunc risky() {}\n")
	out := h.run("", "run", "code-risk", "src")
	if out.code != 0 || h.requests() != n+1 {
		t.Fatalf("exit %d, %d requests", out.code, h.requests()-n)
	}
	if state, _ := json.Marshal(h.server.Requests()[n].Request.State); !strings.Contains(string(state), "package b") {
		t.Errorf("asked about %s", state)
	}
	contains(t, out.stderr, "! 2 flagged", "2 answers from cache · 2 asked")
}

func TestCacheNoCache(t *testing.T) {
	h := setup(t)
	codeRisk(h, "jev-1.13.0")
	// --no-cache asks every question, and keeps the answers.
	h.run("one\n", "run", "relevance", "-p", "question=x")
	h.run("one\n", "run", "relevance", "-p", "question=x", "--no-cache")
	if h.requests() != 2 {
		t.Fatalf("%d requests", h.requests())
	}
	h.run("one\ntwo\n", "run", "relevance", "-p", "question=x", "--no-cache")
	h.run("one\ntwo\n", "run", "relevance", "-p", "question=x")
	if h.requests() != 4 {
		t.Fatalf("%d requests; --no-cache kept no answers", h.requests())
	}

	// A resumed run keeps the setting of the run it resumes.
	h.server.FailNext(422)
	out := h.run("one\n", "run", "relevance", "-p", "question=x", "--no-cache")
	if out.code != 1 {
		t.Fatalf("exit %d", out.code)
	}
	out = h.run("", "runs", "resume")
	if out.code != 0 || h.requests() != 6 {
		t.Fatalf("exit %d, %d requests: %s", out.code, h.requests(), out.stderr)
	}
	h.server.FailNext(422)
	h.run("three\n", "run", "relevance", "-p", "question=x")
	h.run("", "runs", "resume")
	h.run("", "runs", "resume")
	if h.requests() != 8 {
		t.Fatalf("%d requests", h.requests())
	}

	// A different parameter is a different question.
	h.run("one\n", "run", "relevance", "-p", "question=y")
	if h.requests() != 9 {
		t.Fatalf("%d requests", h.requests())
	}
}

const twoQuestions = `{
  "name": "mine",
  "description": "Two questions.",
  "questions": {
    "relevant": {"type": "noul", "instructions": "Is this about tools?"},
    "tested": {"type": "noul", "instructions": "Is this tested?"}
  }
}`

func TestCacheAddedQuestion(t *testing.T) {
	h := setup(t)
	codeRisk(h, "jev-1.13.0")
	h.write(".decide/templates/mine/template.json", strings.Replace(twoQuestions,
		`,
    "tested": {"type": "noul", "instructions": "Is this tested?"}`, "", 1))
	if out := h.run("one\ntwo\n", "run", "mine"); out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}

	// Only the new question is asked, in one request per item.
	h.write(".decide/templates/mine/template.json", twoQuestions)
	out := h.run("one\ntwo\n", "run", "mine", "--json")
	reqs := h.server.Requests()[2:]
	if out.code != 0 || len(reqs) != 2 {
		t.Fatalf("exit %d, %d requests", out.code, len(reqs))
	}
	for _, r := range reqs {
		if len(r.Request.Questions) != 1 || r.Request.Questions["tested"] == nil {
			t.Errorf("asked %v", r.Request.Questions)
		}
	}
	for _, line := range jsonLines(t, out.stdout, true) {
		answers := line["answers"].(map[string]any)
		if !slices.Equal(line["cached"].([]any), []any{"relevant"}) || len(answers) != 2 || line["request_id"] == nil {
			t.Errorf("result: %v", line)
		}
	}
	contains(t, out.stderr, "2 answers from cache · 2 asked")

	// A changed question is a new question.
	h.write(".decide/templates/mine/template.json", strings.Replace(twoQuestions, "tested?", "tested well?", 1))
	h.run("one\ntwo\n", "run", "mine")
	if n := len(h.server.Requests()); n != 6 {
		t.Fatalf("%d requests", n)
	}
}

// versioned answers code-risk questions as a model version would: from
// jev-1.14.0 on, every function is risky.
func versioned(h *harness, model string) {
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Respond(func(req *decide.Request) (*decide.Response, error) {
		risk := 0.1
		if model >= "jev-1.14.0" {
			risk = 0.9
		}
		all := map[string]decide.Answer{
			"risk":            decidetest.NoulAnswer(risk),
			"maintainability": decidetest.ScoreAnswer(levels, 0, 0, 0, 1, 0),
		}
		resp := &decide.Response{Model: model, Answers: map[string]decide.Answer{}}
		for key := range req.Questions {
			resp.Answers[key] = cmp.Or(all[key], decide.Answer(decidetest.NoulAnswer(0.5)))
		}
		return resp, nil
	})
}

func TestCacheModelVersions(t *testing.T) {
	h := setup(t)
	h.write("src/a.go", "package a\n")
	versioned(h, "jev-1.13.0")
	if out := h.run("", "run", "code-risk", "src", "--fail-on", "flagged"); out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}

	// The model behind jev-latest moves. A run with every answer cached
	// sends nothing, so it can't see the upgrade, and reuses the answers.
	versioned(h, "jev-1.14.0")
	out := h.run("", "run", "code-risk", "src", "--fail-on", "flagged", "--dry-run")
	contains(t, out.stdout, "1 item · 2 answers in the cache · 0 to ask")
	if out := h.run("", "run", "code-risk", "src", "--fail-on", "flagged"); out.code != 0 || h.requests() != 1 {
		t.Fatalf("exit %d, %d requests", out.code, h.requests())
	}

	// A run that asks about a new file learns the new version, and asks
	// about the unchanged file again.
	h.write("src/0.go", "package a\n\nvar x = 1\n")
	out = h.run("", "run", "code-risk", "src", "--fail-on", "flagged", "--workers", "1", "--json")
	if out.code != 2 || h.requests() != 3 {
		t.Fatalf("exit %d, %d requests: %s", out.code, h.requests(), out.stderr)
	}
	for _, line := range jsonLines(t, out.stdout, true) {
		if line["cached"] != nil || line["model"] != "jev-1.14.0" {
			t.Errorf("result: %v", line)
		}
	}
	out = h.run("", "run", "code-risk", "src", "--dry-run")
	contains(t, out.stdout, "2 items · 4 answers in the cache · 0 to ask")

	// Cached answers from an old version, in an item whose other question
	// is answered by a new one, are asked again so all come from the new.
	versioned(h, "jev-1.15.0")
	n := h.requests()
	h.write(".decide/templates/mine/template.json", `{"name": "mine", "description": "Risk.",
  "questions": {"risk": {"type": "noul", "instructions": "Is it risky?"}}}`)
	h.run("", "run", "mine", "src/0.go")
	versioned(h, "jev-1.16.0")
	h.write(".decide/templates/mine/template.json", `{"name": "mine", "description": "Risk.",
  "questions": {"risk": {"type": "noul", "instructions": "Is it risky?"},
    "tested": {"type": "noul", "instructions": "Is it tested?"}}}`)
	out = h.run("", "run", "mine", "src/0.go", "--json")
	reqs := h.server.Requests()[n+1:]
	if len(reqs) != 2 || len(reqs[0].Request.Questions) != 1 || reqs[1].Request.Questions["risk"] == nil {
		t.Fatalf("%d requests", len(reqs))
	}
	if line := jsonLines(t, out.stdout, true)[0]; line["cached"] != nil || line["model"] != "jev-1.16.0" {
		t.Fatalf("result: %v", line)
	}
}

func TestCacheKeepsTheNewestAnswer(t *testing.T) {
	h := setup(t)
	for i, p := range []float64{0.9, 0.1, 0.7, 0.2, 0.8, 0.3} {
		h.server.Answer("relevant", decidetest.NoulAnswer(p))
		h.run("one\n", "run", "relevance", "-p", "question=x", "--no-cache")
		h.server.Answer("relevant", decidetest.NoulAnswer(0.5))
		out := h.run("one\n", "run", "relevance", "-p", "question=x", "--json")
		line := jsonLines(t, out.stdout, true)[0]
		got := line["answers"].(map[string]any)["relevant"].(map[string]any)
		if line["cached"] == nil || got["noul"] != p {
			t.Fatalf("round %d: %v, want the answer asked last, %v", i, line, p)
		}
	}
}

func TestCacheChecksKeptAnswers(t *testing.T) {
	h := setup(t)
	h.run("one\n", "run", "relevance", "-p", "question=x")
	// A kept answer that doesn't fit its question is asked again.
	files, _ := filepath.Glob(filepath.Join(template.Home(), "cache", "seg-*.jsonl"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if err := os.WriteFile(f, []byte(strings.ReplaceAll(string(data), `"type":"noul"`, `"type":"choice"`)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := h.run("one\n", "run", "relevance", "-p", "question=x", "--dry-run")
	contains(t, out.stdout, "1 item · 0 answers in the cache · 1 to ask")
	out = h.run("one\n", "run", "relevance", "-p", "question=x", "--json")
	if h.requests() != 2 || jsonLines(t, out.stdout, true)[0]["cached"] != nil {
		t.Fatalf("%d requests: %s", h.requests(), out.stdout)
	}
}

func TestCacheClef(t *testing.T) {
	h := setup(t)
	// Workers AI names no model version, so a cached answer is reused.
	h.server = decidetest.NewServer(t, decidetest.WithResolvedModel(""))
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account-a")
	h.run("one\n", "run", "sentiment", "--provider", "cloudflare")
	out := h.run("one\n", "run", "sentiment", "--provider", "cloudflare", "--json")
	if h.requests() != 1 || jsonLines(t, out.stdout, true)[0]["cached"] == nil {
		t.Fatalf("%d requests: %s", h.requests(), out.stdout)
	}
	out = h.run("one\n", "run", "sentiment", "--provider", "cloudflare", "--dry-run")
	contains(t, out.stdout, "1 item · 1 answer in the cache · 0 to ask")

	// Another account, or another provider, is asked.
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account-b")
	h.run("one\n", "run", "sentiment", "--provider", "cloudflare")
	h.run("one\n", "run", "sentiment")
	if h.requests() != 3 {
		t.Fatalf("%d requests", h.requests())
	}
}

func TestCacheDryRun(t *testing.T) {
	h := setup(t)
	defer func(n int) { source.MaxItemBytes = n }(source.MaxItemBytes)
	source.MaxItemBytes = source.StateRoom + len("src/c.go") + 20
	h.write("src/a.go", "package a\n")
	h.write("src/b.go", "package b\n")
	h.write("src/c.go", "package c\n\nfunc risky() {}\n\nfunc fine() {}\n")
	codeRisk(h, "jev-1.13.0")
	jsonBefore := h.run("", "run", "code-risk", "src", "--dry-run", "--json")

	out := h.run("", "run", "code-risk", "src", "--dry-run")
	contains(t, out.stdout, "3 items · 0 answers in the cache · 6 to ask\n\nand ask each one:")
	h.run("", "run", "code-risk", "src")
	n := h.requests()
	h.write("src/b.go", "package b // changed\n")
	out = h.run("", "run", "code-risk", "src", "--dry-run")
	contains(t, out.stdout, "3 items · 4 answers in the cache · 2 to ask")
	out = h.run("", "run", "code-risk", "src", "--dry-run", "--no-cache")
	contains(t, out.stdout, "3 items · 0 answers in the cache · 6 to ask")
	if h.requests() != n {
		t.Fatalf("a dry run sent %d requests", h.requests()-n)
	}
	h.write("src/b.go", "package b\n")
	if after := h.run("", "run", "code-risk", "src", "--dry-run", "--json"); after.stdout != jsonBefore.stdout {
		t.Errorf("--dry-run --json changed:\n%s\n%s", jsonBefore.stdout, after.stdout)
	}
}

func TestCacheBroken(t *testing.T) {
	h := setup(t)
	codeRisk(h, "jev-1.13.0")
	if err := os.WriteFile(filepath.Join(template.Home(), "cache"), []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out := h.run("one\ntwo\n", "run", "relevance", "-p", "question=x")
		if out.code != 0 || strings.Count(out.stderr, "can't be used") != 1 {
			t.Fatalf("exit %d:\n%s", out.code, out.stderr)
		}
	}
	if h.requests() != 4 {
		t.Fatalf("%d requests", h.requests())
	}
}
