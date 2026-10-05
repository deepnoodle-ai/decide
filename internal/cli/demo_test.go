package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDemo checks that the demo fixture reads as demo/README.md shows. The
// files in demo/repo must match change.diff, or functions are not found.
func TestDemo(t *testing.T) {
	demo, err := filepath.Abs("../../demo")
	if err != nil {
		t.Fatal(err)
	}
	h := setup(t)
	t.Chdir(filepath.Join(demo, "repo"))

	out := h.run("", "run", "code-risk", "../change.diff", "--each", "function", "--dry-run")
	if out.code != 0 || strings.Contains(out.stderr, "Could not find") {
		t.Fatalf("demo/repo does not match demo/change.diff: exit %d: %s", out.code, out.stderr)
	}
	contains(t, out.stdout, "3 functions and 1 hunk", "Refunds.Apply", "Refunds.Search")

	out = h.run("", "run", "security", "admin", "--dry-run")
	contains(t, out.stdout, "8 functions", "Server.TestWebhook", "ResetToken")

	out = h.run("", "run", "prompt-injection", "../change.diff", "--dry-run")
	contains(t, out.stdout, "5 hunks", "docs/integrations.md:1")

	out = h.run("", "run", "task-readiness", "../issues.json", "--dry-run")
	contains(t, out.stdout, "6 records")

	out = h.run("", "run", "pr-description", "../prs.json", "--dry-run")
	contains(t, out.stdout, "5 records")

	out = h.run("", "run", "command-risk", "../commands.txt", "--dry-run")
	contains(t, out.stdout, "10 lines")

	// setup.sh undoes change.diff to make the first commit.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	if err := os.CopyFS(repo, os.DirFS(filepath.Join(demo, "repo"))); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "apply", "-R", "--check", filepath.Join(demo, "change.diff"))
	cmd.Dir = repo
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("change.diff does not undo cleanly in demo/repo: %v\n%s", err, msg)
	}
}
