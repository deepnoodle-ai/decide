package cli

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/decidetest"
)

const formatDiff = "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@ func A() {\n a\n-b\n+c\n@@ -9 +9 @@\n-x\n+y\n"

func TestGitHubFormat(t *testing.T) {
	h := setup(t)
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Answer("risk", decidetest.NoulAnswer(0.88))
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0.1, 0.5, 0.4))

	out := h.run(formatDiff, "run", "code-risk", "--each", "hunk", "--format", "github")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	lines := strings.Split(strings.TrimSpace(out.stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one annotation per flagged hunk, got:\n%s", out.stdout)
	}
	contains(t, lines[0], "::warning title=code-risk flagged risk,file=a.go,line=2::a.go:2%0A! risk: yes 88%25%0A  maintainability: 3.3 of 4")
	contains(t, lines[1], "file=a.go,line=9::")
	contains(t, out.stderr, "! 2 flagged") // the summary still goes to stderr

	report, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, string(report), "### decide code-risk", "**2 answered** · **2 flagged**",
		"| | Item | risk | maintainability |", "| flagged | a.go:2 | **yes 88%** | 3.3 of 4 |")

	// With --fail-on flagged, the annotations are errors, like the job.
	out = h.run(formatDiff, "run", "code-risk", "--each", "hunk", "--format", "github", "--fail-on", "flagged")
	if out.code != 2 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "::error title=code-risk flagged risk,file=a.go,line=2::")

	// Items that are not flagged or matched get no annotation.
	h.server.Answer("risk", decidetest.NoulAnswer(0.1))
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0, 0, 1))
	out = h.run(formatDiff, "run", "code-risk", "--each", "hunk", "--format", "github")
	if out.code != 0 || out.stdout != "" {
		t.Fatalf("exit %d, stdout %q", out.code, out.stdout)
	}

	// Matches are notices, and stdin has no file.
	h.server.Answer("relevant", decidetest.NoulAnswer(0.9))
	out = h.run("pricing\n", "run", "relevance", "-p", "question=x", "--format", "github")
	contains(t, out.stdout, "::notice title=relevance matched relevant::stdin:1%0A● relevant: yes 90%25")
	if strings.Contains(out.stdout, "file=") {
		t.Errorf("stdin is not a file: %s", out.stdout)
	}
}

func TestMarkdownFormat(t *testing.T) {
	h := setup(t)
	h.write("src/a.go", "package a")
	h.write("src/b.go", "package b")
	levels := []any{"0", "1", "2", "3", "4"}
	h.server.Answer("risk", decidetest.NoulAnswer(0.2))
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0, 0, 1))

	out := h.run("", "run", "code-risk", "src", "--format", "md")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "**2 answered** · nothing flagged · typesafe jev-latest · run ",
		"| | Item | risk | maintainability |\n| --- | --- | --- | --- |\n|  | src/a.go | no 80% | 4.0 of 4 |\n")
	if strings.Contains(out.stdout, "<details>") {
		t.Errorf("with nothing flagged, every item is in one table:\n%s", out.stdout)
	}

	// runs view writes the same report for a saved run.
	view := h.run("", "runs", "view", "--format", "md")
	if view.stdout != out.stdout {
		t.Errorf("runs view:\n%s\nwant:\n%s", view.stdout, out.stdout)
	}
}

func TestMarkdownReportOrder(t *testing.T) {
	h := setup(t)
	h.server.Answer("relevant", decidetest.NoulAnswer(0.9))
	out := h.run("one\n", "run", "relevance", "-p", "question=x", "--format", "md")
	contains(t, out.stdout, "**1 answered** · **1 matched**", "| matched | stdin:1 | **yes 90%** |")

	h.server.Answer("relevant", decidetest.NoulAnswer(0.1))
	h.server.FailNext(422)
	out = h.run("one\ntwo\n", "run", "relevance", "-p", "question=x", "--format", "md", "--workers", "1")
	if out.code != 1 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "**1 answered** · **1 failed**", "| failed | stdin:1 |",
		"<details><summary>1 more item</summary>", "|  | stdin:2 | no 90% |")
}

func TestCSVFormat(t *testing.T) {
	h := setup(t)
	h.server.Answer("queue", decidetest.ChoiceAnswer(map[string]float64{"billing": 0.8, "engineering": 0.15, "other": 0.05}))
	out := h.run("=SUM(A1)\n", "run", "ticket-routing", "--format", "csv")
	if out.code != 0 {
		t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	rows, err := csv.NewReader(strings.NewReader(out.stdout)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"source", "status", "flagged", "matched", "queue", "queue_probability", "error"},
		{"stdin:1", "complete", "false", "false", "billing", "0.8000", ""},
	}
	if len(rows) != 2 || strings.Join(rows[0], ",") != strings.Join(want[0], ",") || strings.Join(rows[1], ",") != strings.Join(want[1], ",") {
		t.Fatalf("rows = %q", rows)
	}

	levels := []any{"0", "1", "2", "3", "4"}
	h.write("a.go", "package a")
	h.server.Answer("risk", decidetest.NoulAnswer(0.9))
	h.server.Answer("maintainability", decidetest.ScoreAnswer(levels, 0, 0, 0, 0, 1))
	out = h.run("", "run", "code-risk", "a.go", "--format", "csv")
	contains(t, out.stdout, "source,status,flagged,matched,risk,maintainability,error\na.go,complete,true,false,0.9000,4.0000,\n")
}

func TestFormatErrors(t *testing.T) {
	h := setup(t)
	for _, args := range [][]string{
		{"run", "sentiment", "--json", "--format", "md"},
		{"run", "sentiment", "--format", "csv", "--details"},
		{"run", "sentiment", "--format", "md", "--dry-run"},
		{"runs", "view", "--json", "--format", "csv"},
	} {
		out := h.run("hi\n", args...)
		if out.code == 0 {
			t.Errorf("%v: exit 0, want an error", args)
		}
	}
	// --format json is the same as --json.
	out := h.run("hi\n", "run", "sentiment", "--format", "json", "--dry-run")
	contains(t, out.stdout, `"source":"stdin:1"`)
}

func TestLocation(t *testing.T) {
	for src, want := range map[string]struct {
		file string
		line int
	}{
		"src/server.go":                  {"src/server.go", 0},
		"server.go:42":                   {"server.go", 42},
		"models.py#L88":                  {"models.py", 88},
		"README.md#install":              {"README.md", 0},
		"docs/v1.0#2/guide.md":           {"docs/v1.0#2/guide.md", 0},
		"tickets.jsonl:3":                {"tickets.jsonl", 3},
		"server.go@1a2b3c4:42":           {"server.go", 0},
		"server.go@1a2b3c4#L40":          {"server.go", 0},
		"fixes/auth.patch: server.go:42": {"fixes/auth.patch", 0},
		"stdin:4":                        {"", 0},
		"stdin":                          {"", 0},
		"user@example.com.txt":           {"user@example.com.txt", 0},
	} {
		file, line := location(src)
		if file != want.file || line != want.line {
			t.Errorf("location(%q) = %q, %d; want %q, %d", src, file, line, want.file, want.line)
		}
	}
}

func TestUntrustedTextIsEscaped(t *testing.T) {
	if got := escapeData("50%\n::error::x\r"); got != "50%25%0A::error::x%0D" {
		t.Errorf("escapeData = %q", got)
	}
	if got := escapeProperty("a,b:c\nd"); got != "a%2Cb%3Ac d" {
		t.Errorf("escapeProperty = %q", got)
	}
	if got := mdText("[x](http://e) <img> @team | `a` *b*"); got != `\[x\]\(http://e\) &lt;img&gt; &#64;team \| \`+"`a\\`"+` \*b\*` {
		t.Errorf("mdText = %q", got)
	}
	if got := cells([]string{"=1+1", "+x", "-y", "@z", "ok", "a\nb"}); strings.Join(got, ",") != "'=1+1,'+x,'-y,'@z,ok,a b" {
		t.Errorf("cells = %q", got)
	}
}

func TestGitHubAnnotationCannotInjectCommands(t *testing.T) {
	h := setup(t)
	h.server.Answer("relevant", decidetest.NoulAnswer(0.9))
	h.write("evil\n::error::x.txt", "pricing")
	out := h.run("", "run", "relevance", "-p", "question=x", "--format", "github", "evil\n::error::x.txt")
	for _, line := range strings.Split(strings.TrimSpace(out.stdout), "\n") {
		if !strings.HasPrefix(line, "::notice ") {
			t.Errorf("unexpected line %q in:\n%s", line, out.stdout)
		}
	}
}
