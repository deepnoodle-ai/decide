package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/catalog"
	"github.com/deepnoodle-ai/decide/internal/dataset"
	"github.com/deepnoodle-ai/decide/internal/jobs"
	"github.com/deepnoodle-ai/wonton/tui"
)

func TestFolderTrailPagesAndSelectsNestedSources(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 245; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%03d.go", i)), []byte("package main"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nested, filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for offset := 0; ; {
		p, err := readDirectory(t.Context(), dir, offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Entries) > directoryWindow {
			t.Fatal("unbounded directory")
		}
		for _, e := range p.Entries {
			if seen[e.Name()] {
				t.Fatalf("repeated %s", e.Name())
			}
			seen[e.Name()] = true
		}
		if !p.More {
			break
		}
		offset = p.Next
	}
	if len(seen) != 246 || seen[".git"] || seen["loop"] {
		t.Fatalf("inventory=%d", len(seen))
	}
	s := testScreen(t)
	cmds := s.browse(nested, 0)
	s.HandleEvent(cmds[0]())
	s.browserKey(tui.KeyEvent{Rune: '.'})
	if len(s.options.Sources.Sources) != 1 || s.options.Sources.Sources[0] != nested {
		t.Fatal("folder not selected")
	}
	s.browserKey(tui.KeyEvent{Rune: '.'})
	if len(s.options.Sources.Sources) != 1 {
		t.Fatal("selection duplicated")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readDirectory(canceled, dir, 0); err == nil {
		t.Fatal("directory cancellation ignored")
	}
}

func TestNestedJSONMappingAndFastPreview(t *testing.T) {
	s := testScreen(t)
	skill, err := catalog.LoadSkill("builtin/ticket-routing")
	if err != nil {
		t.Fatal(err)
	}
	s.skill = &skill
	s.options.Skill = "builtin/ticket-routing"
	file := filepath.Join(t.TempDir(), "export.json")
	raw := `{"meta":{"version":1},"a/b":{"~rows":[{"id":1,"ticket":{"description":"charged twice"}},{"id":2,"ticket":{"description":"broken button"}}]}}`
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	s.options.Sources.Sources = []string{file}
	s.HandleEvent(s.preview()())
	s.openJSON()
	if s.jsonTrail == nil {
		t.Fatal("no JSON explorer")
	}
	// Object order is retained, so a/b is the second branch.
	s.jsonTrail.Index = 1
	s.jsonKey(tui.KeyEvent{Key: tui.KeyEnter})
	s.jsonKey(tui.KeyEvent{Rune: 'm'})
	if s.options.Sources.Items != "/a~1b/~0rows" || !s.options.Sources.ItemsSet {
		t.Fatalf("mapping=%+v", s.options.Sources)
	}
	s.HandleEvent(s.preview()())
	if s.problem != "" || len(s.prepared) != 2 {
		t.Fatalf("%s items=%d", s.problem, len(s.prepared))
	}
	s.openJSON()
	s.jsonTrail.Index = 1
	s.jsonKey(tui.KeyEvent{Key: tui.KeyEnter})
	s.jsonKey(tui.KeyEvent{Rune: 'u'})
	if s.options.Sources.State != "/ticket/description" {
		t.Fatal(s.options.Sources.State)
	}
	s.HandleEvent(s.preview()())
	if s.problem != "" || string(s.prepared[0].State) != `"charged twice"` {
		t.Fatalf("%s %s", s.problem, s.prepared[0].State)
	}
}

func TestJSONArrayWindowsAndTrailLimits(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 10000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"n":%d}`, i)
	}
	b.WriteByte(']')
	raw := json.RawMessage(b.String())
	rows, next, more, err := jsonChildren(raw, "/rows", 0)
	if err != nil || len(rows) != 100 || !more || next != 100 {
		t.Fatalf("%d %d %t %v", len(rows), next, more, err)
	}
	rows, next, more, err = jsonChildren(raw, "/rows", 9900)
	if err != nil || len(rows) != 100 || more || next != 10000 || rows[0].Pointer != "/rows/9900" {
		t.Fatalf("%d %d %t %v", len(rows), next, more, err)
	}
	s := testScreen(t)
	s.jsonTrail = &jsonExplorer{Trail: make([]jsonFrame, 64), Entries: []jsonBranch{{Raw: json.RawMessage(`{}`)}}}
	s.jsonKey(tui.KeyEvent{Key: tui.KeyEnter})
	if s.problem == "" || len(s.jsonTrail.Trail) != 64 {
		t.Fatal("unbounded nesting")
	}
}

func TestLargeEvidencePagingSearchAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for i := 0; i < 1105; i++ {
		r := jobs.Result{Version: 1, ID: fmt.Sprintf("item-%04d", i), Status: "complete", Index: i, Data: json.RawMessage(fmt.Sprintf(`{"section":{"row":%d}}`, i))}
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	seen := map[string]bool{}
	for offset := 0; ; {
		p, _, err := loadEvidencePage(t.Context(), path, "", "", offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Results) > windowLimit {
			t.Fatal("unbounded page")
		}
		for _, r := range p.Results {
			if seen[r.ID] {
				t.Fatal("repeated page result")
			}
			seen[r.ID] = true
		}
		if !p.More {
			break
		}
		offset = p.Next
	}
	if len(seen) != 1105 {
		t.Fatal(len(seen))
	}
	p, _, err := loadEvidencePage(t.Context(), path, "", "item-1104", 0)
	if err != nil || len(p.Results) != 1 || p.Results[0].ID != "item-1104" {
		t.Fatalf("%+v %v", p, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := loadEvidencePage(canceled, path, "", "", 0); err == nil {
		t.Fatal("search cancellation ignored")
	}
}

func TestFirstPreviewSkipsMalformedJSONLTailAndRandomIsExplicit(t *testing.T) {
	s := testScreen(t)
	v, err := catalog.LoadSkill("builtin/relevance")
	if err != nil {
		t.Fatal(err)
	}
	s.skill = &v
	s.options.Skill = "builtin/relevance"
	path := filepath.Join(t.TempDir(), "huge.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat("\"valid\"\n", 5)+"not-json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.options.Sources.Sources = []string{path}
	s.HandleEvent(s.preview()())
	if s.problem != "" || len(s.prepared) != 5 {
		t.Fatalf("%s count=%d", s.problem, len(s.prepared))
	}
	s.key(tui.KeyEvent{Rune: 'z'})
	s.HandleEvent(s.preview()())
	if s.problem == "" {
		t.Fatal("random sampling didn't scan entire selection")
	}
}

func TestRootArrayMappingAndRecordedDataReadOnly(t *testing.T) {
	s := testScreen(t)
	s.tab = 2
	s.prepared = []jobs.Prepared{{Item: dataset.Item{Data: json.RawMessage(`[1,2,3]`)}}}
	s.openJSON()
	s.jsonKey(tui.KeyEvent{Rune: 'M'})
	if !s.options.Sources.ItemsSet || s.options.Sources.Items != "" {
		t.Fatal("root array wasn't expanded")
	}
	s.begin("items pointer", "-")
	s.submit("-")
	if s.options.Sources.ItemsSet {
		t.Fatal("whole document mode wasn't restored")
	}
	s.target = "recorded.jsonl"
	s.tab = 3
	s.results = []jobs.Result{{Data: json.RawMessage(`{"rows":[]}`)}}
	s.openJSON()
	s.jsonKey(tui.KeyEvent{Rune: 'm'})
	if s.options.Sources.ItemsSet || s.jsonTrail == nil {
		t.Fatal("saved source mapping changed")
	}
}

func TestCanceledSearchRetainsPriorPageAndFilter(t *testing.T) {
	s := testScreen(t)
	s.target = filepath.Join(t.TempDir(), "evidence.jsonl")
	if err := os.WriteFile(s.target, []byte(`{"decide_run":1,"id":"record","status":"complete","data":{}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.filter = "prior"
	s.results = []jobs.Result{{ID: "old-page"}}
	s.pageHistory = []int{0, 200}
	s.evidenceOffset = 400
	s.begin("filter", "")
	s.submit("new filter")
	cmd := s.pending[0]
	s.pending = nil
	s.workCancel()
	s.HandleEvent(cmd())
	if s.filter != "prior" || len(s.results) != 1 || s.results[0].ID != "old-page" || len(s.pageHistory) != 2 || s.evidenceOffset != 400 {
		t.Fatal("canceled search replaced prior evidence")
	}
}

func TestEvidenceSearchIncludesValidationFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	raw := `{"decide_run":1,"id":"invalid","status":"complete","data":{},"stages":[{"name":"map","state":{},"questions":{"ok":{"type":"noul","instructions":"Does this work?"}},"response":{"answers":{"ok":{"type":"noul","noul":2}}}}]}`
	if err := os.WriteFile(path, []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _, err := loadEvidencePage(t.Context(), path, "", "failed", 0)
	if err != nil || len(p.Results) != 1 || p.Results[0].Status != "failed" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestModelSwitchKeepsFrozenSampleAndResetsProviderSettings(t *testing.T) {
	s := testScreen(t)
	s.sample = []jobs.Prepared{{Item: dataset.Item{ID: "frozen-item"}}}
	s.options.BaseURL = "https://old-provider.example"
	s.options.Profile = "old-profile"
	s.begin("model", "")
	s.submit("cloudflare:clef-flash")
	if s.problem != "" || s.options.Provider != "cloudflare" || s.options.Model != "clef-flash" || s.options.BaseURL != "" || s.options.Profile != "" || len(s.sample) != 1 || s.sample[0].Item.ID != "frozen-item" {
		t.Fatalf("%s %+v", s.problem, s.options)
	}
	s.options.BaseURL = "https://cloudflare.example"
	s.begin("model", "")
	s.submit("clef")
	if s.options.BaseURL != "https://cloudflare.example" {
		t.Fatal("same-provider endpoint lost")
	}
}

func TestJSONTrailReturnsToSelectedChildAndRecoversFromMappingError(t *testing.T) {
	s := testScreen(t)
	var rows []json.RawMessage
	for i := 0; i < 245; i++ {
		rows = append(rows, json.RawMessage(fmt.Sprintf(`{"value":%d}`, i)))
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	s.jsonTrail = &jsonExplorer{Trail: []jsonFrame{{Raw: raw}}}
	s.refreshJSON()
	s.jsonKey(tui.KeyEvent{Rune: 'n'})
	for range 37 {
		s.jsonKey(tui.KeyEvent{Key: tui.KeyArrowDown})
	}
	s.jsonKey(tui.KeyEvent{Key: tui.KeyEnter})
	s.jsonKey(tui.KeyEvent{Rune: 'm'})
	if s.problem == "" {
		t.Fatal("mapping scalar as array must fail")
	}
	s.jsonKey(tui.KeyEvent{Key: tui.KeyArrowLeft})
	if s.jsonTrail.Index != 37 || s.jsonTrail.Trail[0].Offset != 100 || s.problem != "" {
		t.Fatalf("parent selection not restored: %+v, problem=%q", s.jsonTrail, s.problem)
	}
	s.jsonKey(tui.KeyEvent{Rune: 'm'})
	if s.problem == "" {
		t.Fatal("mapping object as array must fail")
	}
	s.jsonKey(tui.KeyEvent{Rune: 'M'})
	if s.problem != "" || s.jsonTrail != nil || !s.options.Sources.ItemsSet || s.options.Sources.Items != "" {
		t.Fatal("successful root-array mapping must clear the earlier error")
	}
}

func TestImportedEvidenceHasHonestCountAndSearchRecovery(t *testing.T) {
	s := testScreen(t)
	s.target = "imported.jsonl"
	s.results = []jobs.Result{{ID: "row-1104", Status: "complete"}}
	s.evidenceOffset = 1104
	content := s.evidenceContent()
	if !strings.Contains(content, "1105–1105") || !strings.Contains(content, "total unknown") || strings.Contains(content, "0 recorded outcomes") {
		t.Fatalf("misleading imported evidence count: %s", content)
	}
	s.results = nil
	s.filter = "missing ticket"
	content = s.evidenceContent()
	if !strings.Contains(content, `No matches for "missing ticket"`) || !strings.Contains(content, "clear it") || strings.Contains(content, "Run a sample") {
		t.Fatalf("missing search recovery guidance: %s", content)
	}
	s.filter = ""
	if content = s.evidenceContent(); !strings.Contains(content, "No saved outcomes") {
		t.Fatalf("missing empty import guidance: %s", content)
	}
}
