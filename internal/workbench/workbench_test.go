package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/jobs"
	"github.com/deepnoodle-ai/wonton/tui"
)

func testScreen(t *testing.T) *screen {
	t.Helper()
	o := jobs.DefaultOptions()
	o.RunDir = t.TempDir()
	o.Skill = "builtin/code-risk"
	s := newScreen(context.Background(), o, "")
	s.initialized = true
	skill, err := catalog.LoadSkill(o.Skill)
	if err != nil {
		t.Fatal(err)
	}
	s.skill = &skill
	t.Cleanup(s.Destroy)
	return s
}

func TestPreviewIsOfflineAndSampleReusesFrozenItems(t *testing.T) {
	s := testScreen(t)
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	s.options.Sources.Sources = []string{path}
	var constructed atomic.Int32
	fake := decidetest.NewServer(t)
	s.options.NewClient = func() (*decide.Client, error) { constructed.Add(1); return fake.Client(t), nil }
	cmd := s.preview()
	s.HandleEvent(cmd())
	if s.problem != "" {
		t.Fatal(s.problem)
	}
	if constructed.Load() != 0 {
		t.Fatal("preview constructed a model client")
	}
	if len(s.sample) != 1 {
		t.Fatalf("sample=%d", len(s.sample))
	}
	before := s.sample[0].Item.Source.Digest
	if err := os.WriteFile(path, []byte("changed after preview"), 0600); err != nil {
		t.Fatal(err)
	}
	cmds := s.run(false)
	if len(cmds) != 1 {
		t.Fatal("sample did not schedule async run")
	}
	s.HandleEvent(cmds[0]())
	if s.problem != "" {
		t.Fatal(s.problem)
	}
	if len(s.results) != 1 || s.results[0].Source.Digest != before {
		t.Fatal("sample did not retain frozen input")
	}
	s.begin("parameters", `{"focus":"concurrency"}`)
	s.submit(s.draft)
	cmds = s.run(false)
	s.HandleEvent(cmds[0]())
	if len(s.experiments) != 2 {
		t.Fatalf("experiments=%d problem=%s", len(s.experiments), s.problem)
	}
	if !strings.Contains(s.compareContent(), "SAME-SAMPLE") {
		t.Fatal("missing comparison")
	}
}

func TestNarrowRenderKeepsHelpAndError(t *testing.T) {
	s := testScreen(t)
	s.width = 40
	s.height = 20
	s.problem = "source file does not exist"
	text := tui.SprintScreen(s.View(), tui.WithWidth(40)).Text()
	for _, want := range []string{"try a judgment", "Choose data", "source file", "Enter open", "q quit"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestEditorValidationRetainsDraftAndOriginalQuestions(t *testing.T) {
	s := testScreen(t)
	before := pretty(s.skill.Questions)
	s.begin("questions", `{"broken":{"type":"nonsense"}}`)
	s.submit(s.draft)
	if s.edit != "questions" || s.problem == "" {
		t.Fatal("invalid editor value accepted")
	}
	if pretty(s.skill.Questions) != before {
		t.Fatal("invalid edit mutated questions")
	}
	var v any
	if strictJSON(`{} garbage`, &v) == nil {
		t.Fatal("invalid trailing JSON accepted")
	}
	s.begin("sources", `{"Sources":["-"]}`)
	s.submit(s.draft)
	if !strings.Contains(s.problem, "keyboard") {
		t.Fatal("stdin allowed in interactive sources")
	}
}

func TestEvidenceFilterScansBeyondDisplayWindowAndExportKeepsAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < windowLimit+20; i++ {
		if err := json.NewEncoder(f).Encode(jobs.Result{Version: 1, ID: fmt.Sprintf("record-%d", i), Status: "complete"}); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	out, _, err := loadEvidence(context.Background(), path, "", "")
	if err != nil || len(out) != windowLimit {
		t.Fatalf("window len=%d err=%v", len(out), err)
	}
	out, _, err = loadEvidence(context.Background(), path, "", "record-219")
	if err != nil || len(out) != 1 {
		t.Fatalf("filter len=%d err=%v", len(out), err)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	if err = exportBundle(context.Background(), bundle, jobs.DefaultOptions(), jobs.Summary{}, path, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "evidence.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "\n") != windowLimit+20 {
		t.Fatal("export truncated to screen window")
	}
}

func TestAsyncCancellationAndTerminalControlSanitization(t *testing.T) {
	s := testScreen(t)
	started := make(chan struct{})
	finished := make(chan tui.Event, 1)
	cmd := s.background(func(ctx context.Context) update {
		close(started)
		<-ctx.Done()
		return update{kind: "preview", err: ctx.Err()}
	})
	go func() { finished <- cmd() }()
	<-started
	if len(s.key(tui.KeyEvent{Key: tui.KeyEscape})) != 0 {
		t.Fatal("escape quit instead of cancel")
	}
	select {
	case e := <-finished:
		s.HandleEvent(e)
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop background work")
	}
	if clean("hello\x1b[2J") != "hello�[2J" {
		t.Fatal("terminal control not sanitized")
	}
}

func TestLegacyEvidenceRevalidatesAnswers(t *testing.T) {
	raw := json.RawMessage(`{"typesafe_cli":1,"id":"old","data":"file","runs":[{"name":"first","command":"judge","state":"file","questions":{"risk":{"type":"noul","instructions":"Risk?"}},"response":{"answers":{"risk":{"type":"noul","probability":2}}}}]}`)
	r, err := legacyResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "failed" || !strings.Contains(r.Stages[0].Error, "validation") {
		t.Fatal("unvalidated legacy evidence shown as successful")
	}
}

func TestLibrarySelectionExportsValidEditableSkill(t *testing.T) {
	s := testScreen(t)
	skills, err := catalog.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	s.skills = skills
	s.tab = 1
	s.index = 0
	s.key(tui.KeyEvent{Key: tui.KeyEnter})
	s.begin("questions", pretty(s.skill.Questions))
	s.submit(s.draft)
	if s.problem != "" {
		t.Fatal(s.problem)
	}
	path := filepath.Join(t.TempDir(), "bundle")
	if err := exportBundle(context.Background(), path, s.currentOptions(), jobs.Summary{}, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.LoadSkill(filepath.Join(path, "skill")); err != nil {
		t.Fatal(err)
	}
}

func TestDestroyDoesNotWaitForDroppedCommand(t *testing.T) {
	s := testScreen(t)
	cmd := s.background(func(ctx context.Context) update { t.Error("dropped command performed work"); return update{} })
	done := make(chan struct{})
	go func() { s.Destroy(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Destroy waited for unscheduled work")
	}
	if cmd().(update).kind != "shutdown" {
		t.Fatal("command ran after shutdown")
	}
}

func TestSimpleSourceAndParameterFlow(t *testing.T) {
	s := testScreen(t)
	s.key(tui.KeyEvent{Rune: 'a'})
	if s.edit != "add source" {
		t.Fatal("source flow requires JSON")
	}
	s.submit("./source directory")
	s.begin("add source", "")
	s.submit("https://example.com/tickets.jsonl")
	s.key(tui.KeyEvent{Rune: 'i'})
	s.submit("**/*.go")
	s.key(tui.KeyEvent{Rune: 'x'})
	s.submit("**/*_test.go")
	if len(s.options.Sources.Sources) != 2 || len(s.options.Sources.Include) != 1 || len(s.options.Sources.Exclude) != 1 {
		t.Fatal("simple source fields did not build selection")
	}
	s.tab = 0
	s.index = 0
	s.key(tui.KeyEvent{Rune: 'd'})
	if len(s.options.Sources.Sources) != 1 || !strings.HasPrefix(s.options.Sources.Sources[0], "https:") {
		t.Fatal("source removal incorrect")
	}
	s.key(tui.KeyEvent{Rune: 't'})
	s.submit("focus=concurrency")
	if s.options.Params["focus"] != "concurrency" {
		t.Fatal("plain parameter edit failed")
	}
	s.begin("parameter", "")
	s.submit("undeclared=bad")
	if s.edit != "parameter" || s.problem == "" {
		t.Fatal("unknown parameter accepted")
	}
}

func TestCredentialsHiddenInScreenAndEditor(t *testing.T) {
	key := "test-ui-key-quoted\"slash\\synthetic"
	t.Setenv("TYPESAFE_API_KEY", key)
	escaped, _ := json.Marshal(key)
	s := testScreen(t)
	s.problem = key + " " + string(escaped)
	text := tui.SprintScreen(s.View(), tui.WithWidth(80)).Text()
	if strings.Contains(text, key) || strings.Contains(text, string(escaped[1:len(escaped)-1])) {
		t.Fatal("credential visible in error rendering")
	}
	s.begin("questions", `{"note":`+string(escaped)+`}`)
	text = tui.SprintScreen(s.View(), tui.WithWidth(80)).Text()
	if containsCredential(text) || containsCredential(s.draft) {
		t.Fatal("credential visible in editor")
	}
	if !strings.Contains(s.draft, "[REDACTED]") {
		t.Fatal("editor did not redact")
	}
}

func TestExportRejectsCredentialConfigurationAndRedactsEvidence(t *testing.T) {
	key := "test-cloudflare-secret\"slash\\synthetic"
	t.Setenv("CLOUDFLARE_AUTH_TOKEN", key)
	s := testScreen(t)
	o := s.currentOptions()
	o.Params = map[string]string{"focus": key}
	path := filepath.Join(t.TempDir(), "rejected")
	if err := exportBundle(context.Background(), path, o, jobs.Summary{}, "", nil); err == nil {
		t.Fatal("credential configuration exported")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected export created artifacts")
	}
	o.Params = nil
	data, _ := json.Marshal(map[string]string{"synthetic_key": key})
	path = filepath.Join(t.TempDir(), "safe")
	if err := exportBundle(context.Background(), path, o, jobs.Summary{}, "", []jobs.Result{{ID: "record", Data: data}}); err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if containsCredential(string(raw)) {
			t.Errorf("credential in export %s", info.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(path, "evidence.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var result jobs.Result
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal("redaction corrupted JSON", err)
	}
	if !strings.Contains(string(result.Data), "[REDACTED]") {
		t.Fatal("evidence key not redacted")
	}
}

func TestBusyFilterDoesNotReplaceRunCancellation(t *testing.T) {
	s := testScreen(t)
	started := make(chan struct{})
	done := make(chan tui.Event, 1)
	cmd := s.background(func(ctx context.Context) update {
		close(started)
		<-ctx.Done()
		return update{kind: "run", err: ctx.Err()}
	})
	go func() { done <- cmd() }()
	<-started
	if got := s.key(tui.KeyEvent{Rune: '/'}); len(got) != 0 || s.edit != "" {
		t.Fatal("busy filter scheduled work")
	}
	s.begin("filter", "late")
	s.submit(s.draft)
	if !s.busy || len(s.pending) != 0 {
		t.Fatal("filter stole cancellation ownership")
	}
	s.begin("export", "unused")
	s.submit(s.draft)
	if !s.busy || len(s.pending) != 0 {
		t.Fatal("export stole cancellation ownership")
	}
	s.edit = ""
	s.key(tui.KeyEvent{Key: tui.KeyEscape})
	select {
	case e := <-done:
		s.HandleEvent(e)
	case <-time.After(time.Second):
		t.Fatal("Esc did not cancel original run")
	}
}

func TestLegacyMetadataAndInvalidAnswersPreserved(t *testing.T) {
	raw := json.RawMessage(`{"typesafe_cli":1,"id":"old","data":"text","runs":[{"name":"judgment","command":"judge","requested_model":"example-model","request_id":"example-request","state":"text","questions":{"risk":{"type":"noul","instructions":"Risk?"}},"response":{"answers":{"risk":{"type":"noul","noul":0.5}}},"invalid":{"risk":{"message":"recorded rejection"}}}]}`)
	r, err := legacyResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stages[0].Model != "example-model" || r.Stages[0].RequestID != "example-request" || len(r.Stages[0].Invalid) != 1 || r.Status != "failed" {
		t.Fatal("legacy metadata/validation lost")
	}
}

func TestImportedAnswerValidationAndTransformation(t *testing.T) {
	r := jobs.Result{Version: 1, ID: "tampered", Status: "complete", Stages: []jobs.Evidence{{Name: "map", State: json.RawMessage(`"text"`), Questions: map[string]json.RawMessage{"risk": json.RawMessage(`{"type":"noul","instructions":"Risk?"}`)}, Response: json.RawMessage(`{"answers":{"risk":{"type":"noul","noul":2}}}`)}}}
	raw, _ := json.Marshal(r)
	var imported jobs.Result
	if err := decodeEvidence(context.Background(), strings.NewReader(string(raw)), func(r jobs.Result) error { imported = r; return nil }); err != nil {
		t.Fatal(err)
	}
	if imported.Status != "failed" {
		t.Fatal("tampered complete status trusted")
	}
	local := jobs.Evidence{Name: "rank", Result: json.RawMessage(`{"position":1}`)}
	if checkedResult(jobs.Result{Status: "complete", Stages: []jobs.Evidence{local}}).Status != "complete" {
		t.Fatal("local transform falsely invalidated")
	}
	s := testScreen(t)
	s.tab = 3
	s.results = []jobs.Result{{Status: "complete", Stages: []jobs.Evidence{local}}}
	if !strings.Contains(s.evidenceContent(), "Local transformation") {
		t.Fatal("local stage represented as provider request")
	}
	if err := decodeEvidence(context.Background(), strings.NewReader(`{"decide_run":2,"id":"future","status":"complete"}`), func(jobs.Result) error { return nil }); err == nil {
		t.Fatal("unsupported version accepted")
	}
}

func TestEvidenceMemoryBudgets(t *testing.T) {
	var records []jobs.Result
	payload := json.RawMessage(`"` + strings.Repeat("x", 2<<20) + `"`)
	for i := 0; i < 10; i++ {
		records = appendWindow(records, jobs.Result{ID: fmt.Sprint(i), Data: payload})
	}
	if resultsBytes(records) > 16<<20 || len(records) >= 10 {
		t.Fatal("result window exceeded byte budget")
	}
	tooLarge := strings.NewReader(`{"decide_run":1,"id":"large","data":"` + strings.Repeat("x", 17<<20) + `"}`)
	if err := decodeEvidence(context.Background(), tooLarge, func(jobs.Result) error { return nil }); err == nil {
		t.Fatal("unbounded record accepted")
	}
}

func TestFrozenPatternExportRetainsResolvedConfiguration(t *testing.T) {
	s := testScreen(t)
	source := filepath.Join(t.TempDir(), "file.go")
	if err := os.WriteFile(source, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	s.options.Sources.Sources = []string{source}
	s.options.Pattern = "builtin/heads"
	s.options.BaseURL = "https://example.invalid/frozen"
	s.options.AccountID = "synthetic-account"
	s.options.Provider = "cloudflare"
	s.options.Model = "clef-flash"
	fake := decidetest.NewServer(t)
	s.options.NewClient = func() (*decide.Client, error) { return fake.Client(t), nil }
	sum, err := jobs.Run(context.Background(), s.options, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "changed-after-run")
	path := filepath.Join(t.TempDir(), "frozen")
	if err := exportBundle(context.Background(), path, s.options, sum, "", nil); err != nil {
		t.Fatal(err)
	}
	pattern, err := catalog.LoadPattern(filepath.Join(path, "pattern.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, skillPath := range pattern.Branches {
		if _, err := catalog.LoadSkill(skillPath); err != nil {
			t.Fatal(err)
		}
	}
	cmd, err := os.ReadFile(filepath.Join(path, "command.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{sum.Model, "--base-url", s.options.BaseURL, "--account-id", s.options.AccountID, "pattern.json"} {
		if !strings.Contains(string(cmd), want) {
			t.Fatalf("missing frozen value %q", want)
		}
	}
	if strings.Contains(string(cmd), "changed-after-run") {
		t.Fatal("export used changed model environment")
	}
}

func TestOrdinaryFrozenExportReplaysOriginalSkillAfterEdit(t *testing.T) {
	s := testScreen(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "input.txt")
	skillPath := filepath.Join(dir, "skill.json")
	if err := os.WriteFile(source, []byte("some code"), 0600); err != nil {
		t.Fatal(err)
	}
	skill := catalog.Skill{Version: 1, Name: "project-test", Description: "Synthetic export test", Inputs: []string{"text"}, State: "value", Parameters: map[string]catalog.Parameter{"focus": {Type: "string", Default: json.RawMessage(`"default"`)}}, Questions: map[string]json.RawMessage{"risk": json.RawMessage(`{"type":"noul","instructions":"Check {{focus}}"}`)}}
	if err := os.WriteFile(skillPath, []byte(pretty(skill)), 0600); err != nil {
		t.Fatal(err)
	}
	o := jobs.DefaultOptions()
	o.RunDir = s.options.RunDir
	o.Skill = skillPath
	o.Sources.Sources = []string{source}
	o.Params = map[string]string{"focus": "literal {{other}}"}
	fake := decidetest.NewServer(t)
	o.NewClient = func() (*decide.Client, error) { return fake.Client(t), nil }
	sum, err := jobs.Run(context.Background(), o, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte(`{"broken":"definition changed after run"}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export")
	if err := exportBundle(context.Background(), path, o, sum, "", nil); err != nil {
		t.Fatal(err)
	}
	exported, err := catalog.LoadSkill(filepath.Join(path, "skill"))
	if err != nil {
		t.Fatal(err)
	}
	if !exported.Resolved {
		t.Fatal("resolved skill not marked frozen")
	}
	replay := jobs.DefaultOptions()
	replay.RunDir = t.TempDir()
	replay.Skill = filepath.Join(path, "skill")
	replay.Sources = o.Sources
	replay.NewClient = o.NewClient
	if _, err := jobs.Run(context.Background(), replay, nil, nil); err != nil {
		t.Fatal("frozen replay failed", err)
	}
	requests := fake.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	if pretty(requests[0].Request.Questions) != pretty(requests[1].Request.Questions) {
		t.Fatal("frozen replay changed original questions")
	}
	cmd, err := os.ReadFile(filepath.Join(path, "command.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cmd), "--pattern") || strings.Contains(string(cmd), "--param") {
		t.Fatal("ordinary frozen skill replay uses pattern or repeats parameter interpolation")
	}
}
