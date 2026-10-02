package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide/internal/skill"
)

// tree creates files under a temporary directory and changes into it.
func tree(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

func walk(t *testing.T, paths []string, stdin string, opts Options) ([]Item, []string) {
	t.Helper()
	var items []Item
	var warnings []string
	opts.Warn = func(msg string) { warnings = append(warnings, msg) }
	err := Walk(context.Background(), paths, strings.NewReader(stdin), opts, func(it Item) error {
		items = append(items, it)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return items, warnings
}

func labels(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func states(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, string(it.State))
	}
	return out
}

func TestFileSkillWalksDirectories(t *testing.T) {
	tree(t, map[string]string{
		".git/config":        "x",
		".gitignore":         "vendor/\n*.log\n",
		"main.go":            "package main",
		"main_test.go":       "package main",
		"README.md":          "# hi",
		"debug.log":          "noise",
		"vendor/dep/dep.go":  "package dep",
		"pkg/util/util.go":   "package util",
		"pkg/util/blob.bin":  "a\x00b",
		"pkg/.decideignore":  "generated.go\n",
		"pkg/generated.go":   "package pkg",
		"pkg/util/notes.txt": "notes",
	})
	items, warnings := walk(t, []string{"."}, "", Options{Input: skill.Text, Each: skill.EachFile})
	want := []string{"README.md", "main.go", "main_test.go", "pkg/util/notes.txt", "pkg/util/util.go"}
	if got := labels(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "blob.bin (binary file)") {
		t.Fatalf("warnings = %v", warnings)
	}
	var state map[string]string
	json.Unmarshal(items[1].State, &state)
	if state["path"] != "main.go" || state["language"] != "go" || state["content"] != "package main" {
		t.Fatalf("state = %v", state)
	}

	items, _ = walk(t, []string{"."}, "", Options{Input: skill.Text, Each: skill.EachFile, Include: []string{"*.go"}, Exclude: []string{"*_test.go"}})
	if got := labels(items); !reflect.DeepEqual(got, []string{"main.go", "pkg/util/util.go"}) {
		t.Fatalf("filtered labels = %v", got)
	}
	items, _ = walk(t, []string{"pkg"}, "", Options{Input: skill.Text, Each: skill.EachFile, Include: []string{"util/*.go"}})
	if got := labels(items); !reflect.DeepEqual(got, []string{"pkg/util/util.go"}) {
		t.Fatalf("subdirectory labels = %v", got)
	}
}

func TestNamedFilesAreNotSkippedSilently(t *testing.T) {
	tree(t, map[string]string{"blob.bin": "a\x00b", "photo.txt": "text"})
	for _, tc := range []struct {
		input skill.Input
		path  string
		want  string
	}{
		{skill.Text, "blob.bin", "binary file"},
		{skill.Image, "photo.txt", "not an image"},
		{skill.Text, "missing.go", "does not exist"},
	} {
		err := Walk(context.Background(), []string{tc.path}, nil, Options{Input: tc.input}, func(Item) error { return nil })
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s as %s: %v, want %q", tc.path, tc.input, err, tc.want)
		}
	}
}

func TestRecords(t *testing.T) {
	tree(t, map[string]string{
		"tickets.jsonl": `{"id":1,"body":"one"}` + "\n\n" + `{"id":2,"body":"two"}` + "\n",
		"export.json":   `{"data":{"tickets":[{"body":"a"},{"body":"b"}]}}`,
		"array.json":    `["x","y"]`,
		"notes.txt":     "first\n\nsecond\n",
	})
	items, _ := walk(t, []string{"tickets.jsonl", "array.json", "notes.txt"}, "", Options{Input: skill.Text})
	if got := labels(items); !reflect.DeepEqual(got, []string{"tickets.jsonl:1", "tickets.jsonl:3", "array.json[0]", "array.json[1]", "notes.txt:1", "notes.txt:3"}) {
		t.Fatalf("labels = %v", got)
	}
	if got := states(items); got[4] != `"first"` || got[2] != `"x"` {
		t.Fatalf("states = %v", got)
	}

	items, _ = walk(t, []string{"export.json"}, "", Options{Input: skill.Text, Items: "data.tickets", Field: "body"})
	if got := states(items); !reflect.DeepEqual(got, []string{`"a"`, `"b"`}) {
		t.Fatalf("items/field states = %v", got)
	}
	if string(items[0].Value) != `{"body":"a"}` {
		t.Fatalf("value = %s", items[0].Value)
	}
	items, _ = walk(t, []string{"export.json"}, "", Options{Input: skill.Text, Items: "/data/tickets", Field: "/body"})
	if len(items) != 2 {
		t.Fatalf("JSON Pointer paths found %d items", len(items))
	}

	err := Walk(context.Background(), []string{"tickets.jsonl"}, nil, Options{Input: skill.Text, Field: "title"}, func(Item) error { return nil })
	if err == nil || !strings.Contains(err.Error(), `no field "title" (found: body, id)`) {
		t.Fatalf("missing field: %v", err)
	}
}

func TestStdinFormats(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"lines", "hello\n\nworld\n", []string{`"hello"`, `"world"`}},
		{"jsonl", `{"a":1}` + "\n" + `{"a":2}`, []string{`{"a":1}`, `{"a":2}`}},
		{"document", "[\n  \"x\",\n  \"y\"\n]\n", []string{`"x"`, `"y"`}},
		{"bracketed text", "[INFO] started\n[WARN] disk full\n", []string{`"[INFO] started"`, `"[WARN] disk full"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, warnings := walk(t, nil, tc.input, Options{Input: skill.Text})
			if got := states(items); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("states = %v, want %v", got, tc.want)
			}
			if wantWarning := tc.name == "bracketed text"; wantWarning != (len(warnings) == 1) {
				t.Fatalf("warnings = %v", warnings)
			}
		})
	}
	items, _ := walk(t, []string{"-"}, "package main", Options{Input: skill.Text, Each: skill.EachFile})
	if len(items) != 1 || items[0].Label != "stdin" {
		t.Fatalf("file from stdin = %v", labels(items))
	}
}

func TestImages(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16)
	tree(t, map[string]string{"a.png": png, "b.txt": "text", "c.jpg": "not really"})
	items, warnings := walk(t, []string{"."}, "", Options{Input: skill.Image})
	if got := labels(items); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Fatalf("labels = %v", got)
	}
	if items[0].Image == nil || items[0].Image.ContentType != "image/png" {
		t.Fatalf("image = %+v", items[0].Image)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "c.jpg") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestLimitAndSample(t *testing.T) {
	var input strings.Builder
	for i := range 100 {
		input.WriteString(strings.Repeat("x", i+1) + "\n")
	}
	items, _ := walk(t, nil, input.String(), Options{Input: skill.Text, Limit: 3})
	if got := labels(items); !reflect.DeepEqual(got, []string{"stdin:1", "stdin:2", "stdin:3"}) {
		t.Fatalf("limit = %v", got)
	}
	first, _ := walk(t, nil, input.String(), Options{Input: skill.Text, Sample: 5})
	again, _ := walk(t, nil, input.String(), Options{Input: skill.Text, Sample: 5})
	if len(first) != 5 || !reflect.DeepEqual(labels(first), labels(again)) {
		t.Fatalf("samples = %v and %v", labels(first), labels(again))
	}
	for i := 1; i < len(first); i++ {
		if len(first[i].State) <= len(first[i-1].State) {
			t.Fatalf("sample is not in input order: %v", labels(first))
		}
	}
}

func TestLabelsOutsideWorkingDirectory(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "marker")
	if err := os.MkdirAll(filepath.Join(outside, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.py", "pkg/util.py"} {
		if err := os.WriteFile(filepath.Join(outside, name), []byte("x = 1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree(t, map[string]string{"main.go": "package main"})
	paths := []string{outside, filepath.Join(outside, "app.py"), "main.go"}
	items, _ := walk(t, paths, "", Options{Input: skill.Text, Each: skill.EachFile})
	want := []string{"marker/app.py", "marker/pkg/util.py", "app.py", "main.go"}
	if got := labels(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	if !strings.Contains(string(items[0].State), `"path":"marker/app.py"`) {
		t.Fatalf("state = %s", items[0].State)
	}
}

func TestLabelsTellOutsideFoldersApart(t *testing.T) {
	outside := t.TempDir()
	for _, name := range []string{"a/src/x.py", "b/src/x.py"} {
		p := filepath.Join(outside, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x = 1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree(t, map[string]string{"main.go": "package main"})
	if err := os.Symlink(filepath.Join(outside, "a/src/x.py"), "link.py"); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	paths := []string{filepath.Join(outside, "a/src"), filepath.Join(outside, "b/src"), "link.py"}
	items, _ := walk(t, paths, "", Options{Input: skill.Text, Each: skill.EachFile})
	want := []string{"a/src/x.py", "b/src/x.py", "link.py"}
	if got := labels(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestDefaultUnits(t *testing.T) {
	tree(t, map[string]string{
		"notes.txt":   "one\ntwo\n",
		"guide.md":    "# Guide\n\nText.\n",
		"main.go":     "package main",
		"data.csv":    "id,body\n1,\"a, b\"\n2,c\n",
		"t.jsonl":     `{"a":1}` + "\n",
		"export.json": `[1]`,
	})
	items, _ := walk(t, []string{"."}, "", Options{Input: skill.Text})
	var got []string
	for _, it := range items {
		got = append(got, it.Label+" "+it.Unit)
	}
	want := []string{"data.csv:2 record", "data.csv:3 record", "export.json[0] record", "guide.md file", "main.go file", "notes.txt:1 line", "notes.txt:2 line", "t.jsonl:1 record"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if string(items[0].State) != `{"id":"1","body":"a, b"}` {
		t.Fatalf("csv record = %s", items[0].State)
	}
	// --each file reads datasets whole; --each line splits documents.
	items, _ = walk(t, []string{"t.jsonl", "guide.md"}, "", Options{Input: skill.Text, Each: skill.EachFile})
	if got := labels(items); !reflect.DeepEqual(got, []string{"t.jsonl", "guide.md"}) {
		t.Fatalf("--each file = %v", got)
	}
	items, _ = walk(t, []string{"guide.md"}, "", Options{Input: skill.Text, Each: skill.EachLine})
	if got := labels(items); !reflect.DeepEqual(got, []string{"guide.md:1", "guide.md:3"}) {
		t.Fatalf("--each line = %v", got)
	}
	items, _ = walk(t, []string{"data.csv"}, "", Options{Input: skill.Text, Field: "body"})
	if got := states(items); !reflect.DeepEqual(got, []string{`"a, b"`, `"c"`}) {
		t.Fatalf("csv --field = %v", got)
	}
}

func TestCSVRowsMustMatchTheHeader(t *testing.T) {
	tree(t, map[string]string{"bad.csv": "a,b\n1\n"})
	err := Walk(context.Background(), []string{"bad.csv"}, nil, Options{Input: skill.Text}, func(Item) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "bad.csv:2 has 1 fields, but the header has 2") {
		t.Fatalf("err = %v", err)
	}
}

// texts returns the content the model sees for each item.
func texts(t *testing.T, items []Item) []string {
	t.Helper()
	var out []string
	for _, it := range items {
		var s struct{ Section, Content string }
		if err := json.Unmarshal(it.State, &s); err != nil {
			t.Fatalf("state %s: %v", it.State, err)
		}
		if s.Section != "" {
			s.Content = "[" + s.Section + "] " + s.Content
		}
		out = append(out, s.Content)
	}
	return out
}

func TestMarkdownParagraphs(t *testing.T) {
	doc := "---\ntitle: Notes\n---\n" + // front matter
		"# Changelog\n\n" +
		"Intro that wraps\nonto a second line.\n\n" +
		"## [1.0.0]\n\n" +
		"- **Tools.** Added tool calling,\n  with approvals.\n" +
		"- Second item\n  - nested detail\n\n  more about the second item\n" +
		"  ```go\n  x := 1\n\n  ```\n" + // a fence inside the list item
		"1. Numbered\n\n" +
		"Setext heading\n--------------\n\n" +
		"<!-- a comment\nover two lines -->\n" +
		"<details>\n<summary>More</summary>\n</details>\n\n" +
		"````md\n```\nnot closed yet\n````\n" +
		"    indented code\n\n" +
		"| a | b |\n|---|---|\n" +
		"***\n" +
		"Last line\r\n"
	tree(t, map[string]string{"CHANGELOG.md": doc})
	items, _ := walk(t, []string{"CHANGELOG.md"}, "", Options{Input: skill.Text, Each: skill.EachParagraph})
	wantLabels := []string{"CHANGELOG.md:6", "CHANGELOG.md:11", "CHANGELOG.md:13", "CHANGELOG.md:21", "CHANGELOG.md:38", "CHANGELOG.md:41"}
	if got := labels(items); !reflect.DeepEqual(got, wantLabels) {
		t.Fatalf("labels = %v, want %v", got, wantLabels)
	}
	want := []string{
		"[Changelog] Intro that wraps\nonto a second line.",
		"[Changelog › [1.0.0]] - **Tools.** Added tool calling,\n  with approvals.",
		"[Changelog › [1.0.0]] - Second item\n  - nested detail\n\n  more about the second item\n  ```go\n  x := 1\n\n  ```",
		"[Changelog › [1.0.0]] 1. Numbered",
		"[Changelog › Setext heading] | a | b |\n|---|---|",
		"[Changelog › Setext heading] Last line",
	}
	if got := texts(t, items); !reflect.DeepEqual(got, want) {
		t.Fatalf("paragraphs:\n%q\nwant\n%q", got, want)
	}
}

func TestFrontMatterNeedsAClosingLine(t *testing.T) {
	tree(t, map[string]string{"a.md": "---\nNot front matter.\n"})
	items, _ := walk(t, []string{"a.md"}, "", Options{Input: skill.Text, Each: skill.EachParagraph})
	if got := texts(t, items); !reflect.DeepEqual(got, []string{"Not front matter."}) {
		t.Fatalf("paragraphs = %q", got)
	}
}

func TestMarkdownSections(t *testing.T) {
	doc := "Preamble.\n\n" +
		"# Guide\n\n" + // nothing under it before the next heading
		"## Install\n\nRun it.\n\n```sh\ngo install\n```\n\n" +
		"## Install\n\nAgain.\n\n" +
		"### Café & Bar!\n\nText.\n"
	tree(t, map[string]string{"guide.md": doc})
	items, _ := walk(t, []string{"guide.md"}, "", Options{Input: skill.Text, Each: skill.EachSection})
	want := []string{"guide.md:1", "guide.md#install", "guide.md#install-1", "guide.md#café--bar"}
	if got := labels(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	wantText := []string{
		"Preamble.",
		"[Guide › Install] Run it.\n\n```sh\ngo install\n```",
		"[Guide › Install] Again.",
		"[Guide › Install › Café & Bar!] Text.",
	}
	if got := texts(t, items); !reflect.DeepEqual(got, wantText) {
		t.Fatalf("sections:\n%q\nwant\n%q", got, wantText)
	}
	if string(items[1].Value) != `"Guide › Install"` {
		t.Fatalf("section preview = %s", items[1].Value)
	}
}

func TestSectionsNeedHeadings(t *testing.T) {
	tree(t, map[string]string{"app.py": "x = 1\n", "docs/a.md": "# A\n\nText.\n", "docs/b.md": "No headings.\n"})
	err := Walk(context.Background(), []string{"app.py"}, nil, Options{Input: skill.Text, Each: skill.EachSection}, func(Item) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "app.py has no headings") {
		t.Fatalf("err = %v", err)
	}
	items, warnings := walk(t, []string{"docs"}, "", Options{Input: skill.Text, Each: skill.EachSection})
	if got := labels(items); !reflect.DeepEqual(got, []string{"docs/a.md#a", "docs/b.md"}) {
		t.Fatalf("labels = %v", got)
	}
	if len(warnings) != 1 || warnings[0] != "1 file has no headings, so it was judged whole" {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestLargeItemsHaveParts(t *testing.T) {
	defer func(n int) { MaxItemBytes = n }(MaxItemBytes)
	MaxItemBytes = 40
	doc := "# A\n\nfirst paragraph here\n\n## B\n\nsecond paragraph here\n\n" + strings.Repeat("x", 90) + "\n"
	tree(t, map[string]string{"big.md": doc, "small.md": "# A\n\nfits\n"})
	items, _ := walk(t, []string{"big.md", "small.md"}, "", Options{Input: skill.Text})
	if len(items) != 2 || len(items[1].Parts) != 0 || items[1].State == nil {
		t.Fatalf("items = %+v", items)
	}
	var got []string
	for _, p := range items[0].Parts {
		var s struct{ Section, Lines, Part, Content string }
		json.Unmarshal(p.State, &s)
		if p.Lines != s.Lines {
			t.Fatalf("part lines %q, state lines %q", p.Lines, s.Lines)
		}
		got = append(got, fmt.Sprintf("%s|%s|%s|%d", s.Lines, s.Part, s.Section, len(s.Content)))
	}
	// Parts break between blocks, and the long line is cut into pieces.
	want := []string{"1-6|1 of 5|A|31", "7-8|2 of 5|A › B|21", "9|3 of 5|A › B|40", "9|4 of 5|A › B|40", "9|5 of 5|A › B|10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	if items[0].State != nil {
		t.Fatalf("an item with parts has state %s", items[0].State)
	}
}

func TestStdinByParagraph(t *testing.T) {
	items, _ := walk(t, nil, "one\ntwo\n\nthree\n", Options{Input: skill.Text, Each: skill.EachParagraph})
	if got := labels(items); !reflect.DeepEqual(got, []string{"stdin:1", "stdin:4"}) {
		t.Fatalf("labels = %v", got)
	}
}
