package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/jobs"
)

func datasetEnvironment(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DECIDE_RUNS_DIR", filepath.Join(dir, "runs"))
	t.Setenv("DECIDE_HOME", filepath.Join(dir, "home"))
	t.Setenv("DECIDE_PROVIDER", "")
	t.Setenv("DECIDE_MODEL", "")
	t.Setenv("DECIDE_PROFILE", "")
	return dir
}

func TestDatasetCommandsPreviewAndPrepareWithoutClient(t *testing.T) {
	dir := datasetEnvironment(t)
	file := filepath.Join(dir, "code.go")
	if e := os.WriteFile(file, []byte("package main\nfunc add(a,b int)int{return a+b}"), 0600); e != nil {
		t.Fatal(e)
	}
	a, out, diagnostics := coreApp(t, "", nil)
	for _, args := range [][]string{{"sources", "list", file}, {"sources", "preview", file}, {"run", "--plan", "builtin/code-risk", file, "--param", "focus=unsafe writes"}} {
		out.Reset()
		diagnostics.Reset()
		if code := a.Run(t.Context(), args); code != 0 {
			t.Fatalf("%v code=%d %s", args, code, diagnostics)
		}
		if out.Len() == 0 {
			t.Fatalf("no output for %v", args)
		}
		var record map[string]any
		if e := json.Unmarshal(out.Bytes(), &record); e != nil {
			t.Fatalf("%v: %s", e, out)
		}
	}
	if !strings.Contains(out.String(), "unsafe writes") || !strings.Contains(out.String(), "questions") {
		t.Fatal(out.String())
	}
}

func TestDurableRunCLIResumeAndExport(t *testing.T) {
	dir := datasetEnvironment(t)
	server := decidetest.NewServer(t)
	a, out, diagnostics := coreApp(t, "\"restore the session\"\n\"banana bread\"\n", server)
	if code := a.Run(t.Context(), []string{"run", "builtin/relevance", "-", "--workers", "1", "--max-requests", "1", "--snapshot", "copy"}); code != 2 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	summaries, e := jobs.List(filepath.Join(dir, "runs"))
	if e != nil || len(summaries) != 1 {
		t.Fatalf("%+v %v", summaries, e)
	}
	id := summaries[0].ID
	if summaries[0].Requests != 1 {
		t.Fatalf("%+v", summaries[0])
	}
	if len(server.Requests()) != 1 {
		t.Fatal("request ceiling failed")
	}
	out.Reset()
	diagnostics.Reset()
	if code := a.Run(t.Context(), []string{"runs", "resume", id, "--max-requests", "3"}); code != 0 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	if len(server.Requests()) != 2 {
		t.Fatalf("successful record repeated: %+v", server.Requests())
	}
	out.Reset()
	if code := a.Run(t.Context(), []string{"runs", "export", id}); code != 0 {
		t.Fatal(diagnostics.String())
	}
	if lines := bytes.Count(out.Bytes(), []byte{'\n'}); lines != 2 {
		t.Fatalf("export=%s", out)
	}
	out.Reset()
	if code := a.Run(t.Context(), []string{"runs", "show", id, "--json"}); code != 0 {
		t.Fatal(diagnostics.String())
	}
	var summary jobs.Summary
	if e := json.Unmarshal(out.Bytes(), &summary); e != nil {
		t.Fatal(e)
	}
	if summary.Completed != 2 || summary.Status != "complete" {
		t.Fatalf("%+v", summary)
	}
}

func TestSkillLiveExamplesUseSuppliedReader(t *testing.T) {
	datasetEnvironment(t)
	server := decidetest.NewServer(t)
	a, out, diagnostics := coreApp(t, "", server)
	if code := a.Run(t.Context(), []string{"skills", "test", "builtin/relevance", "--live"}); code != 0 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	if len(server.Requests()) == 0 || out.Len() == 0 {
		t.Fatal("example execution did not produce model evidence")
	}
}

func TestRootArraySourceFlagAndErrorStream(t *testing.T) {
	dir := datasetEnvironment(t)
	path := filepath.Join(dir, "items.json")
	if e := os.WriteFile(path, []byte(`["first","second"]`), 0600); e != nil {
		t.Fatal(e)
	}
	a, out, diagnostics := coreApp(t, "", nil)
	if code := a.Run(t.Context(), []string{"sources", "list", path, "--items", ""}); code != 0 {
		t.Fatal(diagnostics.String())
	}
	if bytes.Count(out.Bytes(), []byte{'\n'}) != 2 {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"run", "--plan", "builtin/code-risk", path, "--wat"}, {"run", "builtin/relevance", "--workers", "zero"}, {"sources", "list", "--format", "oops", path}} {
		out.Reset()
		diagnostics.Reset()
		if code := a.Run(t.Context(), args); code != 2 {
			t.Fatalf("%v code=%d", args, code)
		}
		if out.Len() != 0 {
			t.Fatalf("usage error polluted stdout: %s", out)
		}
	}
}

func TestLibraryCommandsCreateValidateAndOfflineTest(t *testing.T) {
	datasetEnvironment(t)
	t.Chdir(t.TempDir())
	a, out, diagnostics := coreApp(t, "", nil)
	for _, args := range [][]string{{"skills", "list", "--json"}, {"patterns", "list", "--json"}, {"skills", "new", "project/custom", "--from", "builtin/relevance"}, {"skills", "validate", "project/custom"}, {"skills", "test", "project/custom"}, {"skills", "show", "project/custom", "--json"}} {
		out.Reset()
		diagnostics.Reset()
		if code := a.Run(t.Context(), args); code != 0 {
			t.Fatalf("%v code=%d %s", args, code, diagnostics)
		}
	}
	if !strings.Contains(out.String(), `"questions"`) {
		t.Fatal(out.String())
	}
}

func TestConnectionProfilePrecedenceAndProviderIsolation(t *testing.T) {
	dir := datasetEnvironment(t)
	path := filepath.Join(dir, "settings.json")
	if e := os.WriteFile(path, []byte(`{"profiles":{"vision":{"provider":"cloudflare","model":"clef-flash","account_id":"sample-account"}}}`), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("DECIDE_CONFIG", path)
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "jev-conflicting")
	t.Setenv("TYPESAFE_BASE_URL", "https://wrong.example")
	t.Setenv("CLOUDFLARE_BASE_URL", "https://cloudflare.example")
	o := envOptions()
	o.Profile = "vision"
	if e := resolveConnection(&o); e != nil {
		t.Fatal(e)
	}
	if o.Provider != "cloudflare" || o.Model != "clef-flash" || o.BaseURL != "https://cloudflare.example" {
		t.Fatalf("%+v", o)
	}
	o = envOptions()
	o.Profile = "vision"
	o.Model = "clef"
	if e := resolveConnection(&o); e != nil || o.Model != "clef" {
		t.Fatalf("%+v %v", o, e)
	}
}

func TestDatasetOutputRefusesToOverwriteSource(t *testing.T) {
	dir := datasetEnvironment(t)
	path := filepath.Join(dir, "source.jsonl")
	content := []byte("\"hello\"\n")
	if e := os.WriteFile(path, content, 0600); e != nil {
		t.Fatal(e)
	}
	a, _, diagnostics := coreApp(t, "", nil)
	if code := a.Run(t.Context(), []string{"run", "builtin/relevance", path, "--output", path}); code != 2 {
		t.Fatalf("code=%d %s", code, diagnostics)
	}
	b, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(b, content) {
		t.Fatal("source overwritten")
	}
}

func TestRunSummaryDetailsAndExplicitJSONL(t *testing.T) {
	dir := datasetEnvironment(t)
	path := filepath.Join(dir, "record.jsonl")
	secretData := "ORIGINAL_CONTENT_SHOULD_NOT_FILL_THE_TERMINAL"
	if err := os.WriteFile(path, []byte(`{"description":"`+secretData+`"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"summary", "details", "jsonl", "output"} {
		t.Run(mode, func(t *testing.T) {
			server := decidetest.NewServer(t)
			a, out, diagnostics := coreApp(t, "", server)
			args := []string{"run", "builtin/relevance", path}
			output := filepath.Join(t.TempDir(), "results.jsonl")
			switch mode {
			case "details":
				args = append(args, "--details")
			case "jsonl":
				args = append(args, "--jsonl")
			case "output":
				args = append(args, "--output", output)
			}
			if code := a.Run(t.Context(), args); code != 0 {
				t.Fatalf("code=%d %s", code, diagnostics)
			}
			if mode == "jsonl" {
				var result jobs.Result
				if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Data) == 0 {
					t.Fatalf("JSONL missing full data: %v", err)
				}
			} else {
				if strings.Contains(out.String(), secretData) || strings.Contains(out.String(), `"decide_run"`) {
					t.Fatalf("default/details dumped evidence: %s", out)
				}
				if !strings.Contains(out.String(), "1 complete") || !strings.Contains(out.String(), "Saved evidence:") {
					t.Fatalf("missing useful summary: %s", out)
				}
				if !strings.Contains(out.String(), "% yes") {
					t.Fatalf("missing readable answers: %s", out)
				}
				if mode == "output" {
					b, err := os.ReadFile(output)
					if err != nil || !strings.Contains(string(b), secretData) {
						t.Fatal("explicit file lost full evidence")
					}
				}
			}
		})
	}
}

func TestSavedRunsViewIsReadableOfflineAndSupportsJSONL(t *testing.T) {
	dir := datasetEnvironment(t)
	source := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(source, []byte("restore the session"), 0600); err != nil {
		t.Fatal(err)
	}
	server := decidetest.NewServer(t)
	a, out, diagnostics := coreApp(t, "", server)
	if code := a.Run(t.Context(), []string{"run", "relevance", source}); code != 0 {
		t.Fatalf("run: %d %s", code, diagnostics)
	}
	runs, err := jobs.List(filepath.Join(dir, "runs"))
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs: %v %v", runs, err)
	}
	calls := len(server.Requests())
	a.NewClient = func() (*decide.Client, error) { t.Fatal("view constructed a provider client"); return nil, nil }
	out.Reset()
	if code := a.Run(t.Context(), []string{"runs", "view", runs[0].ID}); code != 0 {
		t.Fatalf("view: %d %s", code, diagnostics)
	}
	if !strings.Contains(out.String(), "note.txt") || !strings.Contains(out.String(), "% yes") || strings.Contains(out.String(), `"decide_run"`) {
		t.Fatalf("unreadable view: %s", out)
	}
	out.Reset()
	if code := a.Run(t.Context(), []string{"runs", "view", runs[0].Path, "--jsonl", "--color", "always"}); code != 0 {
		t.Fatalf("JSONL view: %d %s", code, diagnostics)
	}
	var result jobs.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.ID == "" || bytes.Contains(out.Bytes(), []byte("\x1b")) {
		t.Fatalf("invalid evidence: %v %s", err, out)
	}
	export := filepath.Join(t.TempDir(), "old.jsonl")
	if err := os.WriteFile(export, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := a.Run(t.Context(), []string{"runs", "view", export}); code != 0 || !strings.Contains(out.String(), "% yes") {
		t.Fatalf("file view: %d %s %s", code, out, diagnostics)
	}
	if len(server.Requests()) != calls {
		t.Fatal("view made model calls")
	}
}

func TestRemovedInteractiveCommandsAreNotAdvertisedOrDispatched(t *testing.T) {
	a, out, diagnostics := coreApp(t, "", nil)
	if code := a.Run(t.Context(), []string{"help"}); code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), "explore") || strings.Contains(out.String(), "inspect") || !strings.Contains(out.String(), "runs view") {
		t.Fatalf("help still advertises the removed interface: %s", out)
	}
	for _, command := range []string{"explore", "inspect"} {
		diagnostics.Reset()
		if code := a.Run(t.Context(), []string{command}); code != 2 || !strings.Contains(diagnostics.String(), "unknown command") {
			t.Fatalf("command remains: %s, code=%d %s", command, code, diagnostics)
		}
	}
}

func TestRunPlanPreviewsWithoutClientOrSavedRun(t *testing.T) {
	dir := datasetEnvironment(t)
	file := filepath.Join(dir, "code.go")
	if err := os.WriteFile(file, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", "")
	for _, args := range [][]string{
		{"run", "--plan", "code-risk", file},
		{"run", "code-risk", file, "--plan"},
		{"run", "--pattern", "map", file, "--plan"},
	} {
		a, out, diagnostics := coreApp(t, "", nil)
		a.NewClient = func() (*decide.Client, error) { t.Fatal("plan constructed a provider client"); return nil, nil }
		if code := a.Run(t.Context(), args); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, diagnostics)
		}
		var prepared jobs.Prepared
		if err := json.Unmarshal(out.Bytes(), &prepared); err != nil || len(prepared.Questions) == 0 {
			t.Fatalf("invalid prepared input: %v %s", err, out)
		}
		if !strings.Contains(diagnostics.String(), "Prepared 1 items. No model calls.") {
			t.Fatal(diagnostics.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "runs")); !os.IsNotExist(err) {
		t.Fatalf("plan created run artifacts: %v", err)
	}
}
