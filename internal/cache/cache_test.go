package cache

import (
	"context"
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
	"time"
)

func answer(n int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"noul","probability":%d}`, n))
}

var questions = map[string][]byte{
	"risk":  []byte(`{"type":"noul","instructions":"Is it risky?"}`),
	"tests": []byte(`{"type":"noul","instructions":"Is it tested?"}`),
}

func TestKeys(t *testing.T) {
	c := Open(t.TempDir(), nil)
	s := c.Scope("typesafe", "", "jev-latest", true)
	it := ItemOf([]byte(`"package a"`), "", nil)
	base := s.Key("jev-1.13.0", it, "risk", questions["risk"])
	if s.Key("jev-1.13.0", it, "risk", questions["risk"]) != base {
		t.Fatal("the same request has two keys")
	}
	other := c.Scope("typesafe", "https://gateway.example", "jev-latest", true)
	differ := map[string]Key{
		"state":    s.Key("jev-1.13.0", ItemOf([]byte(`"package b"`), "", nil), "risk", questions["risk"]),
		"image":    s.Key("jev-1.13.0", ItemOf([]byte(`"package a"`), "image/png", []byte{1}), "risk", questions["risk"]),
		"version":  s.Key("jev-1.14.0", it, "risk", questions["risk"]),
		"key":      s.Key("jev-1.13.0", it, "danger", questions["risk"]),
		"question": s.Key("jev-1.13.0", it, "risk", []byte(`{"type":"noul","instructions":"Is it risky?","criteria":{}}`)),
		"address":  other.Key("jev-1.13.0", it, "risk", questions["risk"]),
		"provider": c.Scope("cloudflare", "", "jev-latest", true).Key("jev-1.13.0", it, "risk", questions["risk"]),
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

func TestStoreAndLookup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c := Open(dir, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Open made the folder before anything was kept: %v", err)
	}
	s := c.Scope("typesafe", "", "jev-latest", true)
	it := ItemOf([]byte(`"x"`), "", nil)
	s.Store("jev-1.13.0", it, questions, map[string]json.RawMessage{"risk": answer(1)})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c = Open(dir, nil)
	s = c.Scope("typesafe", "", "jev-latest", true)
	got := s.Lookup("jev-1.13.0", it, questions)
	if len(got) != 1 || string(got["risk"]) != string(answer(1)) {
		t.Fatalf("lookup = %s", got)
	}
	if s.Has("jev-1.13.0", it, questions) {
		t.Error("Has with a question missing")
	}
	if len(s.Lookup("jev-1.14.0", it, questions)) != 0 {
		t.Error("an answer from another version")
	}
	if off := c.Scope("typesafe", "", "jev-latest", false); len(off.Lookup("jev-1.13.0", it, questions)) != 0 {
		t.Error("lookup with reads off")
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

func TestVersions(t *testing.T) {
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	dir := t.TempDir()
	ctx := context.Background()

	// With no version known, the first request is a probe.
	s := Open(dir, nil).Scope("typesafe", "", "jev-latest", true)
	if v, probe, _ := s.Begin(ctx); v != "" || !probe {
		t.Fatalf("Begin = %q, %v; want a probe", v, probe)
	}
	s.End(true, "jev-1.13.0")
	if v, probe, _ := s.Begin(ctx); v != "jev-1.13.0" || probe {
		t.Fatalf("after the probe, Begin = %q, %v", v, probe)
	}
	// A later response with a new version is used from then on.
	s.End(false, "jev-1.14.0")
	if v, _, _ := s.Begin(ctx); v != "jev-1.14.0" {
		t.Fatalf("after an upgrade, Begin = %q", v)
	}

	// Another process trusts the version within the hour.
	clock = clock.Add(59 * time.Minute)
	s = Open(dir, nil).Scope("typesafe", "", "jev-latest", true)
	if v, probe, _ := s.Begin(ctx); v != "jev-1.14.0" || probe {
		t.Fatalf("within the hour, Begin = %q, %v", v, probe)
	}
	// After an hour, it checks again, and still knows the last version.
	clock = clock.Add(2 * time.Minute)
	s = Open(dir, nil).Scope("typesafe", "", "jev-latest", true)
	if v, probe, _ := s.Begin(ctx); v != "" || !probe {
		t.Fatalf("after an hour, Begin = %q, %v", v, probe)
	}
	if v, trusted := s.LastVersion(); v != "jev-1.14.0" || trusted {
		t.Fatalf("LastVersion = %q, %v", v, trusted)
	}
	s.End(true, "")

	// An exact version is trusted however old.
	s = Open(dir, nil).Scope("typesafe", "", "jev-1.13.0", true)
	_, probe, _ := s.Begin(ctx)
	if !probe {
		t.Fatal("an unknown exact name is not checked once")
	}
	s.End(true, "jev-1.13.0")
	clock = clock.Add(48 * time.Hour)
	s = Open(dir, nil).Scope("typesafe", "", "jev-1.13.0", true)
	if v, probe, _ := s.Begin(ctx); v != "jev-1.13.0" || probe {
		t.Fatalf("exact name, Begin = %q, %v", v, probe)
	}

	// With reads off, nothing waits for a version.
	s = Open(dir, nil).Scope("typesafe", "", "jev-preview", false)
	if v, probe, _ := s.Begin(ctx); v != "" || probe {
		t.Fatalf("reads off, Begin = %q, %v", v, probe)
	}
}

func TestOneProbeAtATime(t *testing.T) {
	s := Open(t.TempDir(), nil).Scope("typesafe", "", "jev-latest", true)
	ctx := context.Background()
	if _, probe, _ := s.Begin(ctx); !probe {
		t.Fatal("no probe")
	}
	var wg sync.WaitGroup
	got := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			v, probe, err := s.Begin(ctx)
			if probe || err != nil {
				t.Errorf("a second probe: %v %v", probe, err)
			}
			got <- v
		})
	}
	time.Sleep(10 * time.Millisecond)
	if len(got) != 0 {
		t.Fatal("a caller did not wait for the probe")
	}
	s.End(true, "jev-1.13.0")
	wg.Wait()
	close(got)
	for v := range got {
		if v != "jev-1.13.0" {
			t.Errorf("version %q", v)
		}
	}

	// A failed probe hands the probe to the next caller.
	s = Open(t.TempDir(), nil).Scope("typesafe", "", "jev-latest", true)
	s.Begin(ctx)
	s.End(true, "")
	if _, probe, _ := s.Begin(ctx); !probe {
		t.Fatal("no probe after a failed one")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.Begin(cctx); err == nil {
		t.Fatal("a canceled wait returned no error")
	}
}

func TestTornLines(t *testing.T) {
	dir := t.TempDir()
	k := Key{1}
	good := fmt.Sprintf(`{"answers":{"%x":{"type":"noul"}}}`, k[:])
	torn := fmt.Sprintf(`{"answers":{"%x":{"ty`, Key{2})
	data := good + "\nnot json\n" + `{"answers":{"short":{}}}` + "\n" + torn
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
	Open(dir, nil).Put(map[Key]json.RawMessage{{1}: answer(1)})
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	var warnings []string
	c := Open(dir, func(msg string) { warnings = append(warnings, msg) })
	c.Put(map[Key]json.RawMessage{{2}: answer(2)})
	s := c.Scope("typesafe", "", "jev-latest", true)
	s.End(false, "jev-1.13.0")
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
	c.Put(map[Key]json.RawMessage{{2}: answer(2)})
	c.Put(map[Key]json.RawMessage{{3}: answer(3)})
	if len(warnings) != 1 || c.Has(Key{1}) {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestMerge(t *testing.T) {
	setMergeAbove(t, 100)
	dir := t.TempDir()
	// One process is still writing its segment, so it can't be merged.
	active := Open(dir, nil)
	active.Put(map[Key]json.RawMessage{{0, 1}: answer(1)})
	for i := range 10 {
		c := Open(dir, nil)
		c.Put(map[Key]json.RawMessage{{1, byte(i)}: answer(i)})
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
	active.Put(map[Key]json.RawMessage{{0, 2}: answer(2)})
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
		s := c.Scope("typesafe", "", "jev-latest", true)
		s.End(false, "jev-1.13.0")
		for i := range each {
			c.Put(map[Key]json.RawMessage{{byte(writer), byte(r), byte(i)}: answer(i)})
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
	if v, _ := c.Scope("typesafe", "", "jev-latest", true).LastVersion(); v != "jev-1.13.0" {
		t.Fatalf("version %q", v)
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
