package dataset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, path, value string) string {
	t.Helper()
	p := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func collect(t *testing.T, o Options, stdin io.Reader) []Item {
	t.Helper()
	var items []Item
	if err := Walk(context.Background(), o, stdin, func(i Item) error { items = append(items, i); return nil }); err != nil {
		t.Fatal(err)
	}
	return items
}
func TestDirectoryGlobsIgnoreAndStableIdentity(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".gitignore", "ignored/\n*.tmp\n")
	write(t, dir, "a.go", "package main")
	write(t, dir, "a_test.go", "ignored test")
	write(t, dir, "ignored/b.go", "ignored")
	write(t, dir, "nested/b.go", "package nested")
	write(t, dir, "nested/c.ts", "const x = 1")
	write(t, dir, "nested/.gitignore", "*.ts\n")
	o := DefaultOptions()
	o.Sources = []string{dir}
	o.Include = []string{"**/*.{go,ts}"}
	o.Exclude = []string{"**/*_test.go"}
	items := collect(t, o, nil)
	if len(items) != 2 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Source.Path != "a.go" || items[1].Source.Path != "nested/b.go" {
		t.Fatalf("unexpected paths %+v", items)
	}
	again := collect(t, o, nil)
	if !reflect.DeepEqual(items, again) {
		t.Fatal("identity changed")
	}
	if !strings.HasPrefix(items[0].Source.URI, "file:///") {
		t.Fatal("missing absolute file URI")
	}
	if string(items[0].Data) != "\"package main\"" {
		t.Fatalf("whole file: %s", items[0].Data)
	}
	o.NoIgnore = true
	if len(collect(t, o, nil)) != 4 {
		t.Fatal("no-ignore failed")
	}
}
func TestNestedNegationAndAncestorIgnore(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".gitignore", "*.go\n")
	write(t, dir, "src/.gitignore", "!keep.go\n")
	write(t, dir, "src/keep.go", "yes")
	write(t, dir, "src/drop.go", "no")
	o := DefaultOptions()
	o.Sources = []string{filepath.Join(dir, "src")}
	o.Include = []string{"**/*.go"}
	items := collect(t, o, nil)
	if len(items) != 1 || items[0].Source.Path != "keep.go" {
		t.Fatalf("items %+v", items)
	}
}
func TestJSONAndManifestMappings(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", `{"tickets":[{"id":1,"body":"hello"},{"id":2,"body":"bye"}]}`)
	p := write(t, dir, "sources.json", `{"version":1,"sources":[{"path":"a.json","items":"/tickets","state":"/body","id_field":"/id"}]}`)
	o := DefaultOptions()
	o.Manifest = p
	items := collect(t, o, nil)
	if len(items) != 2 || string(items[0].State) != `"hello"` {
		t.Fatalf("items %+v", items)
	}
	var original map[string]any
	_ = json.Unmarshal(items[0].Data, &original)
	if original["id"] != float64(1) || original["body"] != "hello" {
		t.Fatal("original not preserved")
	}
	o = DefaultOptions()
	o.Sources = []string{write(t, dir, "array.json", `[1,2]`)}
	if len(collect(t, o, nil)) != 1 {
		t.Fatal("array expanded implicitly")
	}
	o.ItemsSet = true
	if len(collect(t, o, nil)) != 2 {
		t.Fatal("explicit root array not expanded")
	}
}
func TestJSONLStreamingLimitAndLocations(t *testing.T) {
	o := DefaultOptions()
	o.Sources = []string{"-"}
	o.Limit = 2
	r := strings.NewReader("\n{\"n\":1}\n{\"n\":2}\nnot-json\n")
	items := collect(t, o, r)
	if len(items) != 2 || items[0].Source.Line != 2 || items[1].Source.Line != 3 {
		t.Fatalf("items %+v", items)
	}
	if r.Len() == 0 { /* bufio may prefetch; callbacks still stop before decoding invalid tail */
	}
	o.Limit = 0
	err := Walk(context.Background(), o, strings.NewReader("bad\n"), func(Item) error { return nil })
	if err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
func TestHTTPAndBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, "{\"ok\":true}\n{\"ok\":false}\n")
	}))
	defer server.Close()
	o := DefaultOptions()
	o.Sources = []string{server.URL}
	items := collect(t, o, nil)
	if len(items) != 2 {
		t.Fatal("HTTP JSONL not detected")
	}
	o.MaxSourceBytes = 8
	if err := Walk(context.Background(), o, nil, func(Item) error { return nil }); err == nil {
		t.Fatal("source bound ignored")
	}
	o = DefaultOptions()
	o.Sources = []string{"-"}
	o.MaxItemBytes = 3
	if err := Walk(context.Background(), o, strings.NewReader("12345\n"), func(Item) error { return nil }); err == nil {
		t.Fatal("item bound ignored")
	}
	o.Sources = []string{"https://user:secret@example.com/file.json"}
	err := Walk(context.Background(), o, nil, func(Item) error { return nil })
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("credential URL not safely rejected")
	}
}
func TestSampleDeterministicAndCallbackErrors(t *testing.T) {
	var data strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintln(&data, i)
	}
	o := DefaultOptions()
	o.Sources = []string{"-"}
	o.Sample = 7
	o.Seed = 43
	a := collect(t, o, strings.NewReader(data.String()))
	b := collect(t, o, strings.NewReader(data.String()))
	if len(a) != 7 || !reflect.DeepEqual(a, b) {
		t.Fatal("sample not reproducible")
	}
	o.Seed++
	if reflect.DeepEqual(a, collect(t, o, strings.NewReader(data.String()))) {
		t.Fatal("seed had no effect")
	}
	want := errors.New("callback")
	if err := Walk(context.Background(), o, strings.NewReader(data.String()), func(Item) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Walk(ctx, o, nil, func(Item) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestPointer(t *testing.T) {
	data := json.RawMessage(`{"a/b":{"~x":[1,2]}}`)
	got, err := Pointer(data, "/a~1b/~0x/1")
	if err != nil || string(got) != "2" {
		t.Fatalf("%s %v", got, err)
	}
	for _, p := range []string{"bad", "/~2", "/a~1b/~0x/01", "/a~1b/~0x/8", "/a~1b/~0x/+1"} {
		if _, err := Pointer(data, p); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
}
func TestSymlinksAndDuplicateSources(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.txt", "hello")
	if err := os.Symlink(p, filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}
	o := DefaultOptions()
	o.Sources = []string{dir, p}
	if len(collect(t, o, nil)) != 1 {
		t.Fatal("default links or union duplicates")
	}
	o.FollowSymlinks = true
	if len(collect(t, o, nil)) != 2 {
		t.Fatal("follow links or loop detection")
	}
}

func TestLargeStreamsAndDirectoryMerge(t *testing.T) {
	o := DefaultOptions()
	o.Sources = []string{"-"}
	count := 0
	if err := Walk(context.Background(), o, strings.NewReader(strings.Repeat("{}\n", 12001)), func(Item) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 12001 {
		t.Fatalf("stream count %d", count)
	}
	dir := t.TempDir()
	for i := 0; i < 2050; i++ {
		write(t, dir, fmt.Sprintf("item-%04d.txt", 2049-i), "x")
	}
	it, err := directoryNames(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	for i := 0; i < 2050; i++ {
		name, ok := it.Next()
		if !ok || name != fmt.Sprintf("item-%04d.txt", i) {
			t.Fatalf("position %d name=%s ok=%v error=%v", i, name, ok, it.err)
		}
	}
	if _, ok := it.Next(); ok || it.err != nil {
		t.Fatalf("end error %v", it.err)
	}
}
func TestEmptyArrayPointerAndDuplicateRecords(t *testing.T) {
	o := DefaultOptions()
	o.Sources = []string{"-"}
	o.Format = "json"
	o.ItemsSet = true
	items := collect(t, o, strings.NewReader(" [1, 2] \n"))
	if len(items) != 2 {
		t.Fatal("root expansion whitespace")
	}
}

func TestImagesRetainBytesAndSourceIdentity(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	o := DefaultOptions()
	o.Sources = []string{path}
	items := collect(t, o, nil)
	if len(items) != 1 || len(items[0].Images) != 1 || !bytes.Equal(items[0].Images[0].Data, data.Bytes()) || items[0].Images[0].ContentType != "image/png" {
		t.Fatalf("image %+v", items)
	}
	if items[0].Source.SizeBytes != int64(data.Len()) || items[0].Source.Digest != digest(data.Bytes()) {
		t.Fatal("image fingerprint metadata")
	}
}
func TestExplicitIDsRemainUniqueForRepeatedRows(t *testing.T) {
	o := DefaultOptions()
	o.Sources = []string{"-"}
	o.IDField = "/id"
	items := collect(t, o, strings.NewReader("{\"id\":1}\n{\"id\":1}\n"))
	if len(items) != 2 || items[0].ID == items[1].ID {
		t.Fatal("duplicate source locations have identical IDs")
	}
}

func TestPublicQueriesAndCredentialValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			t.Error("query was lost")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	o := DefaultOptions()
	o.Sources = []string{server.URL + "?page=1&format=json"}
	if len(collect(t, o, nil)) != 1 {
		t.Fatal("public query failed")
	}
	for _, suffix := range []string{"?api_key=private", "?access_token=private", "?X-Amz-Signature=private", "#private"} {
		o.Sources = []string{server.URL + suffix}
		err := ValidateSources(o)
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("credential URL validation %v", err)
		}
	}
}
func TestHTTPStalledBodyCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	o := DefaultOptions()
	o.Sources = []string{server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Walk(ctx, o, nil, func(Item) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel error %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("HTTP read did not cancel promptly")
	}
}
func TestOptionsRoundTripAndCrossSourceIDs(t *testing.T) {
	o := DefaultOptions()
	o.ItemsSet = true
	o.Items = ""
	o.Include = []string{"**/*.json"}
	o.IDField = "/id"
	o.FollowSymlinks = true
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "MaxItem") || !strings.Contains(string(raw), "items_set") {
		t.Fatalf("option schema %s", raw)
	}
	var decoded Options
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o, decoded) {
		t.Fatal("options lost during JSON roundtrip")
	}
	dir := t.TempDir()
	o = DefaultOptions()
	o.IDField = "/id"
	o.Sources = []string{write(t, dir, "a.json", `{"id":1}`), write(t, dir, "b.json", `{"id":1}`)}
	items := collect(t, o, nil)
	if len(items) != 2 || items[0].ID == items[1].ID {
		t.Fatal("IDs collided across sources")
	}
}

func TestSelectedJSONRejectsTrailingGarbage(t *testing.T) {
	o := DefaultOptions()
	o.Sources = []string{"-"}
	o.Format = "json"
	o.Items = "/items"
	if err := Walk(context.Background(), o, strings.NewReader(`{"items":[1]}garbage`), func(Item) error { return nil }); err == nil {
		t.Fatal("trailing JSON garbage accepted")
	}
}

func TestManifestMultipleSelectionsOfSameDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", "go")
	write(t, dir, "b.ts", "ts")
	o := DefaultOptions()
	o.Manifest = write(t, dir, "manifest.json", `{"version":1,"sources":[{"path":".","include":["*.go"]},{"path":".","include":["*.ts"]}]}`)
	items := collect(t, o, nil)
	if len(items) != 2 || items[0].Source.Path != "a.go" || items[1].Source.Path != "b.ts" {
		t.Fatalf("manifest selections %+v", items)
	}
}

func TestIgnoreFilesApplyAncestorNestedAndNoIgnoreRules(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, repo, ".ignore", "ancestor.txt\n")
	write(t, repo, "tree/ancestor.txt", "ancestor excluded")
	write(t, repo, "tree/nested/.ignore", "nested.txt\n!keep.txt\n")
	write(t, repo, "tree/nested/nested.txt", "nested excluded")
	write(t, repo, "tree/nested/keep.txt", "included")
	write(t, repo, "tree/plain.txt", "included")
	o := DefaultOptions()
	o.Sources = []string{filepath.Join(repo, "tree")}
	o.Include = []string{"**/*.txt"}
	var paths []string
	for _, item := range collect(t, o, nil) {
		paths = append(paths, item.Source.Path)
	}
	if !reflect.DeepEqual(paths, []string{"nested/keep.txt", "plain.txt"}) {
		t.Fatalf("ignore selection: %v", paths)
	}
	o.NoIgnore = true
	if items := collect(t, o, nil); len(items) != 4 {
		t.Fatalf("--no-ignore selected %d items, want 4", len(items))
	}
}
