package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
)

func TestWontonHelpCoversEveryCommandWithoutModelCalls(t *testing.T) {
	datasetEnvironment(t)
	commands := []string{
		"", "run", "sources", "sources list", "sources preview",
		"skills", "skills list", "skills show", "skills new", "skills edit", "skills validate", "skills test",
		"patterns", "patterns list", "patterns show", "runs", "runs list", "runs view", "runs show", "runs watch", "runs resume", "runs export",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			a, out, diagnostics := coreApp(t, "", nil)
			a.NewClient = func() (*decide.Client, error) { t.Fatal("help constructed a model client"); return nil, nil }
			args := append(strings.Fields(command), "--help")
			if code := a.Run(t.Context(), args); code != 0 || out.Len() == 0 || diagnostics.Len() != 0 {
				t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, code, out, diagnostics)
			}
		})
	}
	a, out, diagnostics := coreApp(t, "", nil)
	if code := a.Run(t.Context(), []string{"rnu"}); code != 2 || !strings.Contains(diagnostics.String(), "run") || out.Len() != 0 {
		t.Fatalf("typo: exit=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
}

func TestWontonSourceArgumentsAndRepeatedGlobs(t *testing.T) {
	dir := datasetEnvironment(t)
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.go", "two.md", "skip.go", "other.txt"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, out, diagnostics := coreApp(t, "", nil)
	args := []string{"sources", "preview", "--include", "**/*.{go,md}", dir, "--include", "**/*.txt", "--exclude", "**/skip.go", "--limit", "2", "--seed", "4294967296"}
	if code := a.Run(t.Context(), args); code != 0 {
		t.Fatalf("exit=%d: %s", code, diagnostics)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || strings.Contains(out.String(), "skip.go") || !strings.Contains(out.String(), "one.go") || !strings.Contains(out.String(), "other.txt") {
		t.Fatal(out.String())
	}
}

func TestWontonExplicitRootArrayAndEndOfFlags(t *testing.T) {
	dir := datasetEnvironment(t)
	t.Chdir(dir)
	if err := os.WriteFile("--color", []byte(`[{"name":"alpha"},{"name":"beta"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	a, out, diagnostics := coreApp(t, "", nil)
	args := []string{"sources", "preview", "--format", "json", "--items", "", "--", "--color"}
	if code := a.Run(t.Context(), args); code != 0 {
		t.Fatalf("exit=%d: %s", code, diagnostics)
	}
	if len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 2 || !strings.Contains(out.String(), "alpha") || !strings.Contains(out.String(), "beta") {
		t.Fatal(out.String())
	}
}

func TestWontonEnvironmentDefaultsAndFlagPrecedence(t *testing.T) {
	dir := datasetEnvironment(t)
	file := filepath.Join(dir, "file.go")
	if err := os.WriteFile(file, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DECIDE_MODEL", "environment-model")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "provider-model")
	for _, test := range []struct {
		flags []string
		model string
	}{
		{nil, "environment-model"}, {[]string{"--model", "flag-model"}, "flag-model"},
	} {
		server := decidetest.NewServer(t)
		a, _, diagnostics := coreApp(t, "", server)
		args := append([]string{"run", "code-risk", file}, test.flags...)
		if code := a.Run(t.Context(), args); code != 0 {
			t.Fatalf("exit=%d: %s", code, diagnostics)
		}
		requests := server.Requests()
		if len(requests) != 1 || requests[0].Request.Model != test.model {
			t.Fatalf("requests=%+v, want model %q", requests, test.model)
		}
	}
}

func TestMainHelpHasOneStandardJudgmentWorkflow(t *testing.T) {
	datasetEnvironment(t)
	a, out, diagnostics := coreApp(t, "", nil)
	if code := a.Run(t.Context(), []string{"--help"}); code != 0 {
		t.Fatalf("exit=%d: %s", code, diagnostics)
	}
	for _, name := range []string{"judge", "grep", "label", "score", "check", "pick", "join", "rank", "pack", "gate", "eval", "explore", "inspect", "plan"} {
		if strings.Contains(out.String(), "\n  "+name+" ") {
			t.Fatalf("main help advertises competing workflow %q: %s", name, out)
		}
	}
	for _, want := range []string{"decide run code-risk", "decide skills list", "decide runs view", "Skills define the questions", "run --pattern"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}

func TestRemovedPrimitiveCommandsAreNotDispatched(t *testing.T) {
	datasetEnvironment(t)
	for _, name := range []string{"judge", "grep", "label", "score", "check", "pick", "join", "rank", "pack", "gate", "eval", "plan"} {
		a, out, diagnostics := coreApp(t, "", nil)
		if code := a.Run(t.Context(), []string{name}); code != 2 || out.Len() != 0 || !strings.Contains(diagnostics.String(), "unknown command") {
			t.Fatalf("%s: exit=%d stdout=%s stderr=%s", name, code, out, diagnostics)
		}
	}
}
