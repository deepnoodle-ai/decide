package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func answer(n int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"noul","probability":%d}`, n))
}

var questions = map[string][]byte{
	"risk":  []byte(`{"type":"noul","instructions":"Is it risky?"}`),
	"tests": []byte(`{"type":"noul","instructions":"Is it tested?"}`),
}

func TestKeys(t *testing.T) {
	src := Source{Provider: "typesafe", Model: "jev-latest"}
	it := ItemOf([]byte(`"package a"`), "", nil)
	base := src.Key(it, "risk", questions["risk"])
	if src.Key(it, "risk", questions["risk"]) != base {
		t.Fatal("the same request has two keys")
	}
	with := func(f func(*Source)) Source { s := src; f(&s); return s }
	differ := map[string]Key{
		"state":    src.Key(ItemOf([]byte(`"package b"`), "", nil), "risk", questions["risk"]),
		"image":    src.Key(ItemOf([]byte(`"package a"`), "image/png", []byte{1}), "risk", questions["risk"]),
		"key":      src.Key(it, "danger", questions["risk"]),
		"question": src.Key(it, "risk", []byte(`{"type":"noul","instructions":"Is it risky?","criteria":{}}`)),
		"model":    with(func(s *Source) { s.Model = "jev-1.13.0" }).Key(it, "risk", questions["risk"]),
		"address":  with(func(s *Source) { s.Address = "https://gateway.example" }).Key(it, "risk", questions["risk"]),
		"provider": with(func(s *Source) { s.Provider = "cloudflare" }).Key(it, "risk", questions["risk"]),
	}
	for what, k := range differ {
		if k == base {
			t.Errorf("a different %s has the same key", what)
		}
	}
	// Fields can't run together.
	if ItemOf([]byte("ab"), "c", nil) == ItemOf([]byte("a"), "bc", nil) {
		t.Error("state and content type ran together")
	}
}

// get returns the answer kept for a key.
func get(c *Cache, k Key) json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return json.RawMessage(c.answers[k].answer)
}

func TestStoreAndLookup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c := Open(dir, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Open made the folder before anything was kept: %v", err)
	}
	src := Source{Provider: "typesafe", Model: "jev-latest"}
	it := ItemOf([]byte(`"x"`), "", nil)
	c.Store(src, it, questions, map[string]json.RawMessage{"risk": answer(1), "other": answer(2)})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c = Open(dir, nil)
	got := c.Lookup(src, it, questions)
	if c.Len() != 1 || len(got) != 1 || string(got["risk"]) != string(answer(1)) {
		t.Fatalf("lookup = %s", got)
	}
	if !c.Has(src.Key(it, "risk", questions["risk"])) || c.Has(src.Key(it, "tests", questions["tests"])) {
		t.Error("Has")
	}
	src.Model = "jev-1.13.0"
	if len(c.Lookup(src, it, questions)) != 0 {
		t.Error("an answer from another model")
	}

	// Files are the user's only.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v", info.Mode().Perm())
	}
	for _, f := range segments(t, dir) {
		info, _ := os.Stat(filepath.Join(dir, f))
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, info.Mode().Perm())
		}
		data, _ := os.ReadFile(filepath.Join(dir, f))
		if strings.Contains(string(data), `"x"`) || strings.Contains(string(data), "Is it risky") {
			t.Errorf("the cache holds item or question text: %s", data)
		}
	}
}

func TestNewestWins(t *testing.T) {
	setMergeAbove(t, 100)
	dir := t.TempDir()
	k := Key{9}
	// Each answer replaces the one before, whatever the segments' random
	// parts sort as, and so does each process's next answer.
	for i := range 20 {
		c := Open(dir, nil)
		c.put(map[Key]json.RawMessage{k: answer(i)})
		c.put(map[Key]json.RawMessage{k: answer(100 + i)})
		c.Close()
		if a := get(Open(dir, nil), k); string(a) != string(answer(100+i)) {
			t.Fatalf("round %d: got %s", i, a)
		}
	}
	// A merge keeps the newest.
	mergeAbove = 3
	Open(dir, nil)
	if n := len(segments(t, dir)); n != 1 {
		t.Fatalf("%d segments after the merge", n)
	}
	c := Open(dir, nil)
	c.put(map[Key]json.RawMessage{k: answer(500)})
	c.Close()
	if a := get(Open(dir, nil), k); string(a) != string(answer(500)) {
		t.Fatalf("after the merge: got %s", a)
	}
	// Lines carry their own time, so an answer kept later wins even in a
	// segment made earlier.
	early, late := Open(dir, nil), Open(dir, nil)
	late.put(map[Key]json.RawMessage{k: answer(600)})
	early.put(map[Key]json.RawMessage{k: answer(700)})
	early.Close()
	late.Close()
	if a := get(Open(dir, nil), k); string(a) != string(answer(700)) {
		t.Fatalf("across segments: got %s", a)
	}
}

func TestTornLines(t *testing.T) {
	dir := t.TempDir()
	k := Key{1}
	good := fmt.Sprintf(`{"answers":{"%x":{"type":"noul"}}}`, k[:])
	torn := fmt.Sprintf(`{"answers":{"%x":{"ty`, Key{2})
	version := `{"name":"abc","model":"jev-1.13.0","seen":"2026-10-04T12:00:00Z"}`
	data := good + "\n" + version + "\nnot json\n" + `{"answers":{"short":{}}}` + "\n" + torn
	if err := os.WriteFile(filepath.Join(dir, "seg-a.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	warned := 0
	c := Open(dir, func(string) { warned++ })
	if warned != 0 || c.Len() != 1 || !c.Has(k) {
		t.Fatalf("warned %d, %d answers", warned, c.Len())
	}
}

func TestBrokenCache(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a folder the user can't read")
	}
	dir := t.TempDir()
	Open(dir, nil).put(map[Key]json.RawMessage{{1}: answer(1)})
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("permissions don't deny reads here, as with CAP_DAC_OVERRIDE")
	}
	var warnings []string
	c := Open(dir, func(msg string) { warnings = append(warnings, msg) })
	c.put(map[Key]json.RawMessage{{2}: answer(2)})
	c.put(map[Key]json.RawMessage{{3}: answer(3)})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "can't be used") {
		t.Fatalf("warnings = %q", warnings)
	}
	if c.Has(Key{1}) || c.Has(Key{2}) {
		t.Fatal("a broken cache answered")
	}

	// A folder that can't be written warns once, on the first write.
	os.Chmod(dir, 0o500)
	warnings = nil
	c = Open(dir, func(msg string) { warnings = append(warnings, msg) })
	if !c.Has(Key{1}) {
		t.Fatal("a read-only cache can still be read")
	}
	c.put(map[Key]json.RawMessage{{2}: answer(2)})
	c.put(map[Key]json.RawMessage{{3}: answer(3)})
	if len(warnings) != 1 || c.Has(Key{1}) {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestMerge(t *testing.T) {
	setMergeAbove(t, 100)
	dir := t.TempDir()
	// One process is still writing its segment, so it can't be merged.
	active := Open(dir, nil)
	active.put(map[Key]json.RawMessage{{0, 1}: answer(1)})
	for i := range 10 {
		c := Open(dir, nil)
		c.put(map[Key]json.RawMessage{{1, byte(i)}: answer(i)})
		c.Close()
	}
	before := len(segments(t, dir))
	mergeAbove = 3
	c := Open(dir, nil)
	after := segments(t, dir)
	if before != 11 || len(after) != 2 {
		t.Fatalf("%d segments before the merge, %d after: %v", before, len(after), after)
	}
	if c.Len() != 11 {
		t.Fatalf("%d answers", c.Len())
	}
	// The active segment keeps its entries, old and new.
	active.put(map[Key]json.RawMessage{{0, 2}: answer(2)})
	active.Close()
	c = Open(dir, nil)
	if c.Len() != 12 || !c.Has(Key{0, 1}) || !c.Has(Key{0, 2}) {
		t.Fatalf("%d answers after the active segment closed", c.Len())
	}
}

func TestConcurrentCaches(t *testing.T) {
	setMergeAbove(t, 2)
	dir := t.TempDir()
	const writers, rounds, each = 8, 5, 20
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() { write(dir, w, rounds, each) })
	}
	wg.Wait()
	check(t, dir, writers, rounds, each)
}

// write opens the cache rounds times, keeping each answers each time, as
// one decide process after another would.
func write(dir string, writer, rounds, each int) {
	for r := range rounds {
		c := Open(dir, nil)
		for i := range each {
			c.put(map[Key]json.RawMessage{{byte(writer), byte(r), byte(i)}: answer(i)})
		}
		c.Close()
	}
}

func check(t *testing.T, dir string, writers, rounds, each int) {
	t.Helper()
	c := Open(dir, func(msg string) { t.Errorf("warning: %s", msg) })
	if c.Len() != writers*rounds*each {
		t.Fatalf("%d answers, want %d", c.Len(), writers*rounds*each)
	}
	if n := len(segments(t, dir)); n > writers+mergeAbove+1 {
		t.Fatalf("%d segments", n)
	}
}

func TestConcurrentProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("segments are merged only where files can be locked")
	}
	if dir := os.Getenv("DECIDE_CACHE_TEST_DIR"); dir != "" {
		w, _ := strconv.Atoi(os.Getenv("DECIDE_CACHE_TEST_WRITER"))
		mergeAbove = 2
		write(dir, w, 5, 20)
		return
	}
	dir := t.TempDir()
	const writers = 6
	cmds := make([]*exec.Cmd, writers)
	for w := range writers {
		cmds[w] = exec.Command(os.Args[0], "-test.run=^TestConcurrentProcesses$", "-test.count=1")
		cmds[w].Env = append(os.Environ(), "DECIDE_CACHE_TEST_DIR="+dir, "DECIDE_CACHE_TEST_WRITER="+strconv.Itoa(w))
		if err := cmds[w].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	setMergeAbove(t, 2)
	check(t, dir, writers, 5, 20)
}

func setMergeAbove(t *testing.T, n int) {
	old := mergeAbove
	mergeAbove = n
	t.Cleanup(func() { mergeAbove = old })
}

func segments(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if isSegment(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}
