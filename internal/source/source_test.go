package source

import (
	"context"
	"encoding/json"
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
	items, warnings := walk(t, []string{"."}, "", Options{Input: skill.File})
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

	items, _ = walk(t, []string{"."}, "", Options{Input: skill.File, Include: []string{"*.go"}, Exclude: []string{"*_test.go"}})
	if got := labels(items); !reflect.DeepEqual(got, []string{"main.go", "pkg/util/util.go"}) {
		t.Fatalf("filtered labels = %v", got)
	}
	items, _ = walk(t, []string{"pkg"}, "", Options{Input: skill.File, Include: []string{"util/*.go"}})
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
		{skill.File, "blob.bin", "binary file"},
		{skill.Image, "photo.txt", "not an image"},
		{skill.File, "missing.go", "does not exist"},
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
	items, _ := walk(t, []string{"tickets.jsonl", "array.json", "notes.txt"}, "", Options{Input: skill.Record})
	if got := labels(items); !reflect.DeepEqual(got, []string{"tickets.jsonl:1", "tickets.jsonl:3", "array.json[0]", "array.json[1]", "notes.txt:1", "notes.txt:3"}) {
		t.Fatalf("labels = %v", got)
	}
	if got := states(items); got[4] != `"first"` || got[2] != `"x"` {
		t.Fatalf("states = %v", got)
	}

	items, _ = walk(t, []string{"export.json"}, "", Options{Input: skill.Record, Items: "data.tickets", Field: "body"})
	if got := states(items); !reflect.DeepEqual(got, []string{`"a"`, `"b"`}) {
		t.Fatalf("items/field states = %v", got)
	}
	if string(items[0].Value) != `{"body":"a"}` {
		t.Fatalf("value = %s", items[0].Value)
	}
	items, _ = walk(t, []string{"export.json"}, "", Options{Input: skill.Record, Items: "/data/tickets", Field: "/body"})
	if len(items) != 2 {
		t.Fatalf("JSON Pointer paths found %d items", len(items))
	}

	err := Walk(context.Background(), []string{"tickets.jsonl"}, nil, Options{Input: skill.Record, Field: "title"}, func(Item) error { return nil })
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
			items, warnings := walk(t, nil, tc.input, Options{Input: skill.Record})
			if got := states(items); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("states = %v, want %v", got, tc.want)
			}
			if wantWarning := tc.name == "bracketed text"; wantWarning != (len(warnings) == 1) {
				t.Fatalf("warnings = %v", warnings)
			}
		})
	}
	items, _ := walk(t, []string{"-"}, "package main", Options{Input: skill.File})
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
	items, _ := walk(t, nil, input.String(), Options{Input: skill.Record, Limit: 3})
	if got := labels(items); !reflect.DeepEqual(got, []string{"stdin:1", "stdin:2", "stdin:3"}) {
		t.Fatalf("limit = %v", got)
	}
	first, _ := walk(t, nil, input.String(), Options{Input: skill.Record, Sample: 5})
	again, _ := walk(t, nil, input.String(), Options{Input: skill.Record, Sample: 5})
	if len(first) != 5 || !reflect.DeepEqual(labels(first), labels(again)) {
		t.Fatalf("samples = %v and %v", labels(first), labels(again))
	}
	for i := 1; i < len(first); i++ {
		if len(first[i].State) <= len(first[i-1].State) {
			t.Fatalf("sample is not in input order: %v", labels(first))
		}
	}
}
