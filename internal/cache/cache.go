// Package cache keeps the answers decide has received, so a run asks again
// only about what changed.
//
// An answer is kept by a key: a hash of the provider and its address, the
// model version that answered, the item's text as sent, its image, and the
// question as sent. The cache keeps hashes and answers, never item text,
// file names or images.
//
// The cache is one folder of segment files, each holding lines of JSON.
// Each process appends to a segment of its own, which it locks while it
// writes, and reads every segment when it opens the cache. Each line
// carries the time it was written, and the newest answer to a key wins,
// so an answer asked fresh replaces an older one. A process that opens a
// cache with many segments merges the ones nobody is writing into one, so
// the folder keeps a few files however many answers it holds. An entry
// seen twice is harmless, so processes that merge at the same time cannot
// lose or damage each other's entries. A line cut short by a crash is
// ignored.
package cache

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// mergeAbove is how many segments a cache may have before Open merges
// the ones that are not being written.
var mergeAbove = 32

// now is the clock, replaced in tests.
var now = time.Now

// Key identifies one answer.
type Key [16]byte

// Item identifies what a request says about one item: the text sent as its
// state and its image, if any.
type Item [32]byte

// ItemOf returns the identity of an item's state, as sent, and image.
func ItemOf(state []byte, contentType string, image []byte) Item {
	h := sha256.New()
	field(h, []byte("item"))
	field(h, state)
	field(h, []byte(contentType))
	field(h, image)
	var it Item
	h.Sum(it[:0])
	return it
}

// field writes b with its length, so that fields cannot run together.
func field(h interface{ Write([]byte) (int, error) }, b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	h.Write(n[:])
	h.Write(b)
}

type version struct {
	model string
	seen  time.Time
}

type entry struct {
	answer string
	time   int64 // when it was kept, in nanoseconds since 1970
}

// line is one line of a segment: answers kept at one time, or the version
// a model name was last seen to resolve to.
type line struct {
	Time    int64                      `json:"time,omitempty"`
	Answers map[string]json.RawMessage `json:"answers,omitempty"`
	Name    string                     `json:"name,omitempty"` // a hash of provider, address and model name
	Model   string                     `json:"model,omitempty"`
	Seen    time.Time                  `json:"seen,omitzero"`
}

// Cache is the answer cache in one folder. It is safe for concurrent use.
// After the first error it warns once and acts as if it were empty.
type Cache struct {
	dir  string
	warn func(string)

	mu       sync.Mutex
	broken   bool
	answers  map[Key]entry
	versions map[string]version
	seg      *os.File // this process's segment, created on the first write
	last     int64    // the time of this process's latest line
}

// Open reads the cache in dir. The folder is made when the first answer
// is kept. Open never fails: a cache that can't be read calls warn and
// acts as if it were empty. warn may be nil.
func Open(dir string, warn func(string)) *Cache {
	c := &Cache{dir: dir, warn: warn, answers: map[Key]entry{}, versions: map[string]version{}}
	if err := c.load(); err != nil {
		c.fail(err)
	}
	return c
}

// fail turns the cache off and warns about the first error.
func (c *Cache) fail(err error) {
	if c.broken {
		return
	}
	c.broken = true
	c.answers, c.versions = map[Key]entry{}, map[string]version{}
	if c.warn != nil {
		c.warn(fmt.Sprintf("The answer cache in %s can't be used, so every question is asked: %v", c.dir, err))
	}
}

func (c *Cache) load() error {
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // made on the first write
	}
	if err != nil {
		return err
	}
	// Segments are named by the time they were made, and read oldest
	// first, so among lines kept at the same time the later one wins.
	var segments []string
	for _, e := range entries {
		if isSegment(e.Name()) {
			segments = append(segments, e.Name())
		}
	}
	merge := len(segments) > mergeAbove
	var idle []*os.File // segments nobody is writing, locked until merged
	defer func() {
		for _, f := range idle {
			f.Close()
		}
	}()
	for _, name := range segments {
		f, err := os.Open(filepath.Join(c.dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue // merged by another process
		}
		if err != nil {
			return err
		}
		if merge && tryLock(f) {
			idle = append(idle, f)
		} else {
			defer f.Close()
		}
		if err := c.read(f); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if len(idle) > 1 {
		c.merge(idle)
		idle = nil // merge closed them
	}
	return nil
}

func isSegment(name string) bool {
	return strings.HasPrefix(name, "seg-") && strings.HasSuffix(name, ".jsonl")
}

// segmentName names a new segment by the time it is made, so names sort
// oldest first.
func segmentName() string {
	return fmt.Sprintf("seg-%020d-%s.jsonl", now().UnixNano(), random())
}

// read adds a segment's lines. A line cut short by a crash, or one that
// can't be decoded, is skipped.
func (c *Cache) read(f *os.File) error {
	br := bufio.NewReader(f)
	for {
		b, err := br.ReadBytes('\n')
		if len(b) > 0 && b[len(b)-1] == '\n' {
			c.add(b)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (c *Cache) add(b []byte) {
	var l line
	if json.Unmarshal(b, &l) != nil {
		return
	}
	for k, a := range l.Answers {
		var key Key
		if hex.DecodedLen(len(k)) != len(key) {
			continue
		}
		if _, err := hex.Decode(key[:], []byte(k)); err != nil || !json.Valid(a) {
			continue
		}
		if e, ok := c.answers[key]; !ok || l.Time >= e.time {
			c.answers[key] = entry{answer: string(a), time: l.Time}
		}
	}
	if l.Name != "" && l.Model != "" {
		if v, ok := c.versions[l.Name]; !ok || !l.Seen.Before(v.seen) {
			c.versions[l.Name] = version{model: l.Model, seen: l.Seen}
		}
	}
}

// merge writes every entry in the cache to one new segment, keeping the
// time each answer was kept, then removes the idle segments it replaces.
// Entries from segments still being written are copied too; an entry in
// two segments is harmless. A merge that fails leaves the segments as they
// were. It closes the idle segments.
func (c *Cache) merge(idle []*os.File) {
	err := c.writeMerged()
	for _, f := range idle {
		f.Close() // before removing: Windows can't remove an open file
		if err == nil {
			os.Remove(f.Name())
		}
	}
}

func (c *Cache) writeMerged() error {
	tmp, err := os.CreateTemp(c.dir, ".merge-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails once renamed
	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	// Answers kept at the same time share a line, as when they were kept.
	byTime := map[int64]map[string]json.RawMessage{}
	for k, e := range c.answers {
		batch := byTime[e.time]
		if batch == nil || len(batch) == 1000 {
			if batch != nil {
				if err = enc.Encode(line{Time: e.time, Answers: batch}); err != nil {
					break
				}
			}
			batch = map[string]json.RawMessage{}
			byTime[e.time] = batch
		}
		batch[hex.EncodeToString(k[:])] = json.RawMessage(e.answer)
	}
	for t, batch := range byTime {
		if err == nil {
			err = enc.Encode(line{Time: t, Answers: batch})
		}
	}
	for name, v := range c.versions {
		if err == nil {
			err = enc.Encode(line{Name: name, Model: v.model, Seen: v.seen})
		}
	}
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(c.dir, segmentName()))
	}
	if err == nil {
		err = syncDir(c.dir)
	}
	return err
}

func random() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Get returns the answer kept for a key.
func (c *Cache) Get(k Key) (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.answers[k]
	if !ok {
		return nil, false
	}
	return json.RawMessage(e.answer), true
}

// Has reports whether the cache holds an answer for a key.
func (c *Cache) Has(k Key) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.answers[k]
	return ok
}

// Put keeps answers, replacing any kept before for the same keys.
func (c *Cache) Put(answers map[Key]json.RawMessage) {
	if len(answers) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Times only grow within a process, even when the clock is coarse.
	t := max(now().UnixNano(), c.last+1)
	l := line{Time: t, Answers: map[string]json.RawMessage{}}
	for k, a := range answers {
		l.Answers[hex.EncodeToString(k[:])] = a
	}
	if c.write(l) {
		c.last = t
		for k, a := range answers {
			c.answers[k] = entry{answer: string(a), time: t}
		}
	}
}

// write appends a line to this process's segment, creating it first.
func (c *Cache) write(l line) bool {
	if c.broken {
		return false
	}
	b, err := json.Marshal(l)
	if err != nil {
		c.fail(err)
		return false
	}
	if c.seg == nil {
		if err := os.MkdirAll(c.dir, 0o700); err != nil {
			c.fail(err)
			return false
		}
		if c.seg, err = newSegment(c.dir); err != nil {
			c.fail(err)
			return false
		}
	}
	// One write per line: a crash can cut short only the last line.
	if _, err := c.seg.Write(append(b, '\n')); err != nil {
		c.fail(err)
		return false
	}
	return true
}

// Close closes this process's segment.
func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seg == nil {
		return nil
	}
	err := c.seg.Close()
	c.seg = nil
	return err
}

// Len is how many answers the cache holds.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.answers)
}

func (c *Cache) version(name string) (version, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.versions[name]
	return v, ok
}

func (c *Cache) setVersion(name string, v version) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.write(line{Name: name, Model: v.model, Seen: v.seen}) {
		c.versions[name] = v
	}
}
