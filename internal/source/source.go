// Package source reads files, directories, and stdin as a stream of items
// for a skill to evaluate.
//
// What counts as one item depends on the skill's input:
//
//   - file: each text file is one item, sent with its path and contents.
//   - record: each line of JSONL or text, each element of a JSON array, or a
//     whole JSON object is one item.
//   - image: each image file is one item.
//
// Directory walks skip hidden files and folders (such as .git and .env),
// files ignored by .gitignore or .decideignore, symbolic links, and files a
// skill cannot read, such as binary files for a file skill. Files named
// directly are always read.
package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	ignore "github.com/sabhiram/go-gitignore"

	"github.com/deepnoodle-ai/decide/internal/skill"
)

// Size limits. Larger files are skipped with a warning.
const (
	MaxFileBytes  = 1 << 20  // text files read whole by file skills
	MaxImageBytes = 4 << 20  // the Cloudflare image limit
	MaxJSONBytes  = 64 << 20 // JSON documents split into records
)

// Options selects and shapes items.
type Options struct {
	Input   skill.Input
	Include []string // globs relative to each directory argument
	Exclude []string
	Items   string // path to the array of records in a JSON document
	Field   string // path to the value to evaluate within each record
	Limit   int    // stop after this many items
	Sample  int    // pick this many items at random
	Warn    func(msg string)
}

// Item is one thing to evaluate.
type Item struct {
	Label string          `json:"source"`          // where it came from, e.g. "src/main.go" or "tickets.jsonl:3"
	Value json.RawMessage `json:"input,omitempty"` // the original record, for record skills
	State json.RawMessage `json:"state"`           // what the model sees
	Image *ImageData      `json:"-"`
}

// ImageData is an image file's contents.
type ImageData struct {
	ContentType string
	Data        []byte
}

// errStop ends a walk early once the limit is reached.
var errStop = errors.New("stop")

// Walk reads items from paths in a stable order and calls fn for each. A
// path of "-" reads stdin. With no paths, Walk reads stdin.
func Walk(ctx context.Context, paths []string, stdin io.Reader, opts Options, fn func(Item) error) error {
	for _, glob := range append(slices.Clone(opts.Include), opts.Exclude...) {
		if !doublestar.ValidatePattern(glob) {
			return fmt.Errorf("%q is not a valid glob pattern", glob)
		}
	}
	if opts.Warn == nil {
		opts.Warn = func(string) {}
	}
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	w := &walker{ctx: ctx, opts: opts, stdin: stdin, names: names(paths)}
	switch {
	case opts.Sample > 0:
		w.emit = w.sampler(opts.Sample)
	default:
		w.emit = func(it Item) error {
			if err := fn(it); err != nil {
				return err
			}
			w.count++
			if opts.Limit > 0 && w.count >= opts.Limit {
				return errStop
			}
			return nil
		}
	}
	for _, p := range paths {
		if err := w.path(p); err != nil {
			if errors.Is(err, errStop) {
				break
			}
			return err
		}
	}
	// Report sampled items in input order.
	slices.SortFunc(w.sample, func(a, b sampled) int { return a.order - b.order })
	for _, s := range w.sample {
		if err := fn(s.item); err != nil {
			return err
		}
	}
	return nil
}

type walker struct {
	ctx    context.Context
	opts   Options
	stdin  io.Reader
	emit   func(Item) error
	names  map[string]string // label for each path argument
	count  int
	seen   int
	sample []sampled
}

// sampler keeps a uniform random sample (reservoir sampling). The seed is
// fixed so that the same command picks the same items.
func (w *walker) sampler(n int) func(Item) error {
	rng := rand.New(rand.NewSource(1))
	return func(it Item) error {
		w.seen++
		if len(w.sample) < n {
			w.sample = append(w.sample, sampled{w.seen, it})
		} else if j := rng.Intn(w.seen); j < n {
			w.sample[j] = sampled{w.seen, it}
		}
		return nil
	}
}

type sampled struct {
	order int
	item  Item
}

func (w *walker) path(p string) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if p == "-" {
		return w.stdinItems()
	}
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s does not exist", p)
		}
		return err
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		return w.file(p, w.names[p], true)
	}
	rules, err := ancestorRules(p)
	if err != nil {
		return err
	}
	return w.dir(w.names[p], p, p, rules)
}

// names chooses how labels name each path argument: by its path from the
// working directory when it is inside it, and otherwise by its last
// elements, as many as it takes to tell the arguments apart, so
// ~/code/marker shows as "marker". Labels are also sent to the model, so
// they never reveal the home directory.
func names(paths []string) map[string]string {
	out := map[string]string{}
	outside := map[string][]string{} // argument -> elements of its absolute path
	for _, p := range paths {
		if p == "-" {
			continue
		}
		if rel, ok := inside(p); ok {
			out[p] = rel
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		outside[p] = strings.Split(filepath.ToSlash(abs), "/")
	}
	for depth := 1; len(outside) > 0; depth++ {
		label := func(p string) string {
			e := outside[p]
			return path.Join(e[max(0, len(e)-depth):]...)
		}
		owners := map[string]map[string]bool{} // label -> the paths it would name
		for p, e := range outside {
			if owners[label(p)] == nil {
				owners[label(p)] = map[string]bool{}
			}
			owners[label(p)][path.Join(e...)] = true
		}
		for p, e := range outside {
			if l := label(p); len(owners[l]) == 1 || depth >= len(e) {
				out[p] = l
				delete(outside, p)
			}
		}
	}
	return out
}

// inside returns p relative to the working directory, if it is inside it.
func inside(p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	if rel, ok := under(wd, abs); ok {
		return rel, true
	}
	// The working directory may be reached through a link, as /tmp is on
	// macOS. Links are resolved only here, so a linked file inside the
	// project keeps the name it was given.
	rwd, err1 := filepath.EvalSymlinks(wd)
	rabs, err2 := filepath.EvalSymlinks(abs)
	if err1 != nil || err2 != nil {
		return "", false
	}
	return under(rwd, rabs)
}

func under(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	return rel, rel != ".." && !strings.HasPrefix(rel, "../")
}

// dir walks a directory. prefix is the label of the path argument root.
func (w *walker) dir(prefix, root, dir string, rules []rule) error {
	local, err := dirRules(dir)
	if err != nil {
		return err
	}
	rules = append(slices.Clone(rules), local...)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		full := filepath.Join(dir, name)
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if ignored(rules, full, e.IsDir()) || match(w.opts.Exclude, rel) {
			continue
		}
		if e.IsDir() {
			if err := w.dir(prefix, root, full, rules); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() || (len(w.opts.Include) > 0 && !match(w.opts.Include, rel)) {
			continue
		}
		label := path.Join(prefix, rel)
		if err := w.file(full, label, false); err != nil {
			return err
		}
	}
	return nil
}

func match(globs []string, rel string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, rel); ok {
			return true
		}
		// A pattern without a slash also matches a file name at any depth,
		// so --include '*.go' means what it looks like.
		if !strings.Contains(g, "/") {
			if ok, _ := doublestar.Match(g, filepath.Base(rel)); ok {
				return true
			}
		}
	}
	return false
}

// file reads one file. explicit is true when the user named the file, in
// which case files the skill cannot read are errors instead of skips.
func (w *walker) file(path, label string, explicit bool) error {
	skip := func(format string, args ...any) error {
		msg := fmt.Sprintf(format, args...)
		if explicit {
			return errors.New(msg)
		}
		w.opts.Warn("Skipped " + msg)
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	switch w.opts.Input {
	case skill.Image:
		if !isImageName(path) {
			if explicit {
				return fmt.Errorf("%s is not an image; this skill reads PNG, JPEG, or WebP files", label)
			}
			return nil
		}
		if info.Size() > MaxImageBytes {
			return skip("%s (images must be at most 4 MiB)", label)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		ctype := http.DetectContentType(data)
		if !strings.HasPrefix(ctype, "image/") {
			return skip("%s (not a readable image)", label)
		}
		state, _ := json.Marshal(map[string]any{"path": label, "content_type": ctype})
		return w.emit(Item{Label: label, State: state, Image: &ImageData{ContentType: ctype, Data: data}})
	case skill.File:
		if info.Size() > MaxFileBytes {
			return skip("%s (larger than 1 MiB)", label)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isBinary(data) {
			return skip("%s (binary file)", label)
		}
		return w.emit(fileItem(label, data))
	default:
		if info.Size() > MaxJSONBytes {
			return skip("%s (larger than 64 MiB)", label)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		head := make([]byte, 8000)
		n, _ := io.ReadFull(f, head)
		if isBinary(head[:n]) {
			return skip("%s (binary file)", label)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return w.records(f, label, formatOf(path))
	}
}

func fileItem(label string, data []byte) Item {
	lang := strings.TrimPrefix(filepath.Ext(label), ".")
	state, _ := json.Marshal(map[string]string{"path": label, "language": lang, "content": string(data)})
	return Item{Label: label, State: state}
}

func (w *walker) stdinItems() error {
	if w.stdin == nil {
		return errors.New("no input on stdin")
	}
	switch w.opts.Input {
	case skill.Image:
		return errors.New("image skills read image files, not stdin")
	case skill.File:
		data, err := io.ReadAll(io.LimitReader(w.stdin, MaxFileBytes+1))
		if err != nil {
			return err
		}
		if len(data) > MaxFileBytes {
			return errors.New("stdin is larger than 1 MiB")
		}
		return w.emit(fileItem("stdin", data))
	}
	br := bufio.NewReader(w.stdin)
	return w.records(br, "stdin", sniff(br))
}

type format int

const (
	lines   format = iota // each nonblank line is a string record
	jsonl                 // each nonblank line is a JSON record
	jsonDoc               // one JSON document; arrays are split into records
)

func formatOf(path string) format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".ndjson":
		return jsonl
	case ".json":
		return jsonDoc
	}
	return lines
}

// sniff guesses the format of piped input: JSONL when the first line is a
// complete JSON value, a JSON document when it starts like one, and plain
// lines of text otherwise.
func sniff(br *bufio.Reader) format {
	peek, _ := br.Peek(64 << 10)
	first := bytes.TrimSpace(peek)
	if i := bytes.IndexByte(first, '\n'); i >= 0 {
		first = bytes.TrimSpace(first[:i])
	}
	if len(first) == 0 {
		return lines
	}
	if (first[0] == '{' || first[0] == '[') && json.Valid(first) {
		return jsonl
	}
	if first[0] == '{' || first[0] == '[' {
		return jsonDoc
	}
	return lines
}

func (w *walker) records(r io.Reader, label string, f format) error {
	if f == jsonDoc {
		data, err := io.ReadAll(io.LimitReader(r, MaxJSONBytes+1))
		if err != nil {
			return err
		}
		if len(data) > MaxJSONBytes {
			return fmt.Errorf("%s is larger than 64 MiB; use JSONL for large datasets", label)
		}
		if label == "stdin" && !json.Valid(data) {
			// Piped text such as "[INFO] started" only looks like JSON.
			w.opts.Warn("stdin is not valid JSON, so each line is read as text")
			return w.records(bytes.NewReader(data), label, lines)
		}
		return w.document(data, label)
	}
	if w.opts.Items != "" {
		return fmt.Errorf("--items only applies to JSON files, not %s", label)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), MaxFileBytes)
	line := 0
	for sc.Scan() {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		line++
		text := bytes.TrimSpace(sc.Bytes())
		if len(text) == 0 {
			continue
		}
		where := label + ":" + strconv.Itoa(line)
		var value json.RawMessage
		if f == jsonl {
			if !json.Valid(text) {
				return fmt.Errorf("%s is not valid JSON", where)
			}
			value = slices.Clone(text)
		} else {
			value, _ = json.Marshal(string(text))
		}
		if err := w.record(value, where); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("%s has a line longer than 1 MiB", label)
		}
		return err
	}
	return nil
}

func (w *walker) document(data []byte, label string) error {
	if !json.Valid(data) {
		return fmt.Errorf("%s is not valid JSON", label)
	}
	value := json.RawMessage(data)
	if w.opts.Items != "" {
		var err error
		if value, err = Lookup(value, w.opts.Items); err != nil {
			return fmt.Errorf("--items %s in %s: %w", w.opts.Items, label, err)
		}
	}
	var elems []json.RawMessage
	if json.Unmarshal(value, &elems) != nil {
		if w.opts.Items != "" {
			return fmt.Errorf("--items %s in %s is not an array", w.opts.Items, label)
		}
		return w.record(bytes.TrimSpace(value), label)
	}
	for i, elem := range elems {
		if err := w.record(elem, label+"["+strconv.Itoa(i)+"]"); err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) record(value json.RawMessage, label string) error {
	state := value
	if w.opts.Field != "" {
		var err error
		if state, err = Lookup(value, w.opts.Field); err != nil {
			return fmt.Errorf("--field %s in %s: %w", w.opts.Field, label, err)
		}
	}
	return w.emit(Item{Label: label, Value: value, State: state})
}

// Lookup finds a value by a dotted path such as "ticket.body" or "items.0".
// A path that starts with "/" is read as a JSON Pointer.
func Lookup(data json.RawMessage, path string) (json.RawMessage, error) {
	var parts []string
	if strings.HasPrefix(path, "/") {
		for _, p := range strings.Split(path[1:], "/") {
			parts = append(parts, strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~"))
		}
	} else {
		parts = strings.Split(path, ".")
	}
	cur := data
	for _, part := range parts {
		var obj map[string]json.RawMessage
		var arr []json.RawMessage
		switch {
		case json.Unmarshal(cur, &obj) == nil:
			next, ok := obj[part]
			if !ok {
				names := make([]string, 0, len(obj))
				for name := range obj {
					names = append(names, name)
				}
				slices.Sort(names)
				return nil, fmt.Errorf("no field %q (found: %s)", part, strings.Join(names, ", "))
			}
			cur = next
		case json.Unmarshal(cur, &arr) == nil:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(arr) {
				return nil, fmt.Errorf("no element %q", part)
			}
			cur = arr[i]
		default:
			return nil, fmt.Errorf("cannot look up %q in a %s", part, kind(cur))
		}
	}
	return cur, nil
}

func kind(v json.RawMessage) string {
	switch t := bytes.TrimSpace(v); {
	case len(t) == 0:
		return "value"
	case t[0] == '"':
		return "string"
	case t[0] == 't' || t[0] == 'f':
		return "boolean"
	case t[0] == 'n':
		return "null"
	}
	return "number"
}

func isImageName(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

type rule struct {
	dir     string
	matcher *ignore.GitIgnore
}

func dirRules(dir string) ([]rule, error) {
	var out []rule
	for _, name := range []string{".gitignore", ".decideignore"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rule{dir, ignore.CompileIgnoreLines(strings.Split(string(data), "\n")...)})
	}
	return out, nil
}

// ancestorRules applies ignore files from parent directories up to the
// enclosing repository, so evaluating ./src still honors the root .gitignore.
func ancestorRules(start string) ([]rule, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		if dir == filepath.Dir(dir) {
			return nil, nil // not in a repository
		}
	}
	var out []rule
	for i := len(dirs) - 1; i >= 0; i-- {
		rules, err := dirRules(dirs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, rules...)
	}
	return out, nil
}

func ignored(rules []rule, path string, isDir bool) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, r := range rules {
		dir, _ := filepath.Abs(r.dir)
		rel, err := filepath.Rel(dir, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		if isDir {
			rel += "/"
		}
		if r.matcher.MatchesPath(rel) {
			return true
		}
	}
	return false
}
