// Package source reads files, directories, and stdin as a stream of items
// for a skill to evaluate.
//
// What counts as one item depends on the unit (Options.Each) and the data:
//
//   - A dataset has one item per record: each line of JSONL, each element
//     of a JSON array (or a whole JSON object), and each row of CSV.
//   - A .txt file or piped text has one item per line.
//   - Any other text file is one item, sent with its path.
//   - With Each, a text file is one item per file, line, paragraph, or
//     Markdown section instead. Datasets are split into records unless
//     Each is "file".
//   - With Each "function", a source file in a language listed by
//     Languages is one item per function or method. Other files are
//     skipped. A file whose structure the scanner cannot follow is one
//     item.
//   - An image skill has one item per image file.
//
// An item too large to send whole is cut into parts, which the caller
// judges separately and combines. A source file is cut between its
// functions.
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
	"encoding/csv"
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
	MaxFileBytes  = 1 << 20  // text files read whole
	MaxImageBytes = 4 << 20  // the Cloudflare image limit
	MaxJSONBytes  = 64 << 20 // JSON documents split into records
)

// MaxItemBytes is the most text sent in one request. Longer items are cut
// into parts. It keeps an item and its questions within Jev's limit of
// 32,000 tokens, at three or more bytes per token.
var MaxItemBytes = 64 << 10

// Options selects and shapes items.
type Options struct {
	Input   skill.Input
	Each    string   // the unit: "file", "line", "paragraph", "section", "function", or "" for the default
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
	Unit  string          `json:"unit"`            // "file", "line", "paragraph", "section", "function", "record", or "image"
	Value json.RawMessage `json:"input,omitempty"` // the original record, a paragraph's text, a section's headings, or a function's name
	State json.RawMessage `json:"state,omitempty"` // what the model sees; empty when the item has parts
	Parts []Part          `json:"parts,omitempty"` // the item cut into pieces, when it is too large to send whole
	Image *ImageData      `json:"-"`
}

// Part is one piece of an item too large to send whole.
type Part struct {
	Lines string          `json:"lines,omitempty"` // the file's lines in this part, such as "120-260"; empty for a record
	Size  int             `json:"size"`            // the size of its text, for weighting answers
	State json.RawMessage `json:"state"`
}

// Units of items that are not text.
const (
	UnitRecord = "record"
	UnitImage  = "image"
)

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
	if w.whole > 0 {
		opts.Warn(fmt.Sprintf("%d %s no Markdown headings, so %s judged whole",
			w.whole, plural(w.whole, "file has", "files have"), plural(w.whole, "it was", "each was")))
	}
	if w.notCode > 0 {
		opts.Warn(fmt.Sprintf("Skipped %d %s not in %s", w.notCode, plural(w.notCode, "file", "files"), orList(Languages())))
	}
	if w.noFuncs > 0 {
		opts.Warn(fmt.Sprintf("Skipped %d %s with no functions", w.noFuncs, plural(w.noFuncs, "file", "files")))
	}
	if n := len(w.lost); n > 0 {
		names := strings.Join(w.lost[:min(n, 3)], ", ")
		if n > 3 {
			names += fmt.Sprintf(", and %d more", n-3)
		}
		opts.Warn(fmt.Sprintf("Could not find the functions in %s, so %s judged whole", names, plural(n, "it was", "each was")))
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
	ctx     context.Context
	opts    Options
	stdin   io.Reader
	emit    func(Item) error
	names   map[string]string // label for each path argument
	count   int
	whole   int      // files read whole for want of sections
	notCode int      // files skipped for want of functions: not source code
	noFuncs int      // source files without functions
	lost    []string // source files read whole because their functions could not be found
	seen    int
	sample  []sampled
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
	if w.opts.Input == skill.Image {
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
		return w.emit(Item{Label: label, Unit: UnitImage, State: state, Image: &ImageData{ContentType: ctype, Data: data}})
	}
	if w.opts.Each == skill.EachFunction {
		lang := languageOf(path)
		if lang == nil {
			if explicit {
				return fmt.Errorf("%s is not in %s, so decide cannot find its functions; use --each file", label, orList(Languages()))
			}
			w.notCode++
			return nil
		}
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
		return w.functions(label, string(data), lang, explicit)
	}
	f := formatOf(path)
	each := w.opts.Each
	if each == "" && f == lines {
		each = skill.EachLine
	}
	if f.dataset() && each != skill.EachFile || each == skill.EachLine {
		if info.Size() > MaxJSONBytes {
			return skip("%s (larger than 64 MiB)", label)
		}
		fh, err := os.Open(path)
		if err != nil {
			return err
		}
		defer fh.Close()
		head := make([]byte, 8000)
		n, _ := io.ReadFull(fh, head)
		if isBinary(head[:n]) {
			return skip("%s (binary file)", label)
		}
		if _, err := fh.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if !f.dataset() {
			f = lines
		}
		return w.records(fh, label, f)
	}
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
	return w.text(label, string(data), f == markdown, each, explicit)
}

// text makes items of a text file read as a document: the whole file, or
// each paragraph or section of it.
func (w *walker) text(label, data string, md bool, each string, explicit bool) error {
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	lang := strings.TrimPrefix(filepath.Ext(label), ".")
	if label == "stdin" {
		lang = ""
	}
	var pieces []piece
	switch each {
	case skill.EachParagraph:
		if md {
			pieces = markdownParagraphs(lines)
		} else {
			pieces = plainParagraphs(lines)
		}
		for _, p := range pieces {
			if err := w.piece(label+":"+strconv.Itoa(p.start), skill.EachParagraph, label, p, lines, md); err != nil {
				return err
			}
		}
		return nil
	case skill.EachSection:
		if md {
			pieces = markdownSections(lines)
		}
		if !md || !hasHeadings(lines) {
			if explicit && label != "stdin" {
				why := "has no headings to divide it into sections"
				if !md {
					why = "is not Markdown, so it has no sections"
				}
				return fmt.Errorf("%s %s; use --each paragraph or --each file", label, why)
			}
			w.whole++
			break
		}
		for _, p := range pieces {
			l := label + ":" + strconv.Itoa(p.start)
			if p.anchor != "" {
				l = label + "#" + p.anchor
			}
			if err := w.piece(l, skill.EachSection, label, p, lines, md); err != nil {
				return err
			}
		}
		return nil
	}
	// The whole file.
	text := strings.Join(lines, "\n")
	budget := budget(label)
	if jsonSize(text) <= budget {
		state, _ := json.Marshal(map[string]string{"path": label, "language": lang, "content": text})
		return w.emit(Item{Label: label, Unit: skill.EachFile, State: state})
	}
	it := Item{Label: label, Unit: skill.EachFile}
	parts := split(lines, 1, len(lines), structureOf(label, lines, md), budget)
	for i, p := range parts {
		state, _ := json.Marshal(partState{Path: label, Language: lang, Section: p.section, Lines: p.lines(),
			Part: fmt.Sprintf("%d of %d", i+1, len(parts)), Content: p.text})
		it.Parts = append(it.Parts, Part{Lines: p.lines(), Size: len(p.text), State: state})
	}
	return w.emit(it)
}

// StateRoom is room left in each request for the rest of an item's state:
// its section, line numbers, and field names.
const StateRoom = 1024

// budget is how much text, encoded as JSON, one request can hold for the
// file at path.
func budget(path string) int {
	return max(MaxItemBytes-StateRoom-jsonSize(path), 16)
}

// preview is the start of a long text, for an item stored in parts.
func preview(text string) string {
	if r := []rune(text); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return text
}

// structureOf says where a file at path may be cut: between Markdown
// blocks, between functions, or after blank lines.
func structureOf(path string, lines []string, md bool) structure {
	if md {
		return markdownStructure(lines)
	}
	if lang := languageOf(path); lang != nil {
		if c, err := lang.find(strings.Join(lines, "\n")); err == nil {
			return codeStructure(lines, c)
		}
	}
	return plainStructure(lines)
}

// functions makes an item of each function in a source file. A file whose
// functions cannot be found is one item.
func (w *walker) functions(label, data string, lang *language, explicit bool) error {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	lines := strings.Split(data, "\n")
	c, err := lang.find(data)
	if err != nil {
		w.lost = append(w.lost, label)
		return w.text(label, data, false, skill.EachFile, explicit)
	}
	if len(c.fns) == 0 {
		if explicit {
			return fmt.Errorf("%s has no functions; use --each file", label)
		}
		w.noFuncs++
		return nil
	}
	ext := strings.TrimPrefix(filepath.Ext(label), ".")
	for _, f := range c.fns {
		context := c.context(lines, f)
		text := span(lines, f.lead, f.end)
		value, _ := json.Marshal(f.name)
		it := Item{Label: fmt.Sprintf("%s#L%d", label, f.start), Unit: skill.EachFunction, Value: value}
		budget := max(budget(label)-jsonSize(context)-jsonSize(f.name), 16)
		if jsonSize(text) <= budget {
			it.State, _ = json.Marshal(partState{Path: label, Language: ext, Function: f.name,
				Lines: piece{start: f.lead, end: f.end}.lines(), Context: context, Content: text})
		} else {
			parts := split(lines, f.lead, f.end, plainStructure(lines), budget)
			for i, p := range parts {
				state, _ := json.Marshal(partState{Path: label, Language: ext, Function: f.name, Lines: p.lines(),
					Part: fmt.Sprintf("%d of %d", i+1, len(parts)), Context: context, Content: p.text})
				it.Parts = append(it.Parts, Part{Lines: p.lines(), Size: len(p.text), State: state})
			}
		}
		if err := w.emit(it); err != nil {
			return err
		}
	}
	return nil
}

// orList joins "Go, Python, and Java" as "Go, Python, or Java".
func orList(and string) string {
	if i := strings.LastIndex(and, " and "); i >= 0 {
		return and[:i] + " or " + and[i+len(" and "):]
	}
	return and
}

func hasHeadings(lines []string) bool {
	for _, b := range parseMarkdown(lines) {
		if b.kind == mdHeading {
			return true
		}
	}
	return false
}

// partState is what the model sees of a paragraph, a section, or a part.
type partState struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Function string `json:"function,omitempty"`
	Section  string `json:"section,omitempty"`
	Lines    string `json:"lines,omitempty"`
	Part     string `json:"part,omitempty"`
	Context  string `json:"context,omitempty"` // code the content needs to be read, such as its imports
	Content  string `json:"content"`
}

// piece emits a paragraph or section, cut into parts when it is too large.
func (w *walker) piece(label, unit, path string, p piece, lines []string, md bool) error {
	value, _ := json.Marshal(p.text)
	if unit == skill.EachSection && p.section != "" {
		value, _ = json.Marshal(p.section) // "Install › macOS" says more than "#macos"
	}
	budget := budget(path)
	if jsonSize(p.text) <= budget {
		state, _ := json.Marshal(partState{Path: path, Section: p.section, Content: p.text})
		return w.emit(Item{Label: label, Unit: unit, Value: value, State: state})
	}
	if unit != skill.EachSection || p.section == "" {
		value, _ = json.Marshal(preview(p.text)) // each part's result keeps a copy
	}
	it := Item{Label: label, Unit: unit, Value: value}
	parts := split(lines, p.start, p.end, structureOf(path, lines, md), budget)
	for i, part := range parts {
		section := part.section
		if section == "" {
			section = p.section
		}
		state, _ := json.Marshal(partState{Path: path, Section: section, Lines: part.lines(),
			Part: fmt.Sprintf("%d of %d", i+1, len(parts)), Content: part.text})
		it.Parts = append(it.Parts, Part{Lines: part.lines(), Size: len(part.text), State: state})
	}
	return w.emit(it)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (w *walker) stdinItems() error {
	if w.stdin == nil {
		return errors.New("no input on stdin")
	}
	if w.opts.Input == skill.Image {
		return errors.New("image skills read image files, not stdin")
	}
	br := bufio.NewReader(w.stdin)
	switch w.opts.Each {
	case skill.EachFunction:
		return errors.New("--each function reads source files, not stdin")
	case skill.EachFile, skill.EachParagraph, skill.EachSection:
		data, err := io.ReadAll(io.LimitReader(br, MaxFileBytes+1))
		if err != nil {
			return err
		}
		if len(data) > MaxFileBytes {
			return errors.New("stdin is larger than 1 MiB")
		}
		if isBinary(data) {
			return errors.New("stdin is not text")
		}
		return w.text("stdin", string(data), true, w.opts.Each, true)
	case skill.EachLine:
		return w.records(br, "stdin", lines)
	}
	return w.records(br, "stdin", sniff(br))
}

type format int

const (
	document format = iota // a text file, one item unless --each says otherwise
	markdown               // a Markdown document
	lines                  // each nonblank line is a string item
	jsonl                  // each nonblank line is a JSON record
	jsonDoc                // one JSON document; arrays are split into records
	csvFile                // each row is a record, named by the header row
)

// dataset reports whether the format holds records.
func (f format) dataset() bool { return f == jsonl || f == jsonDoc || f == csvFile }

func formatOf(path string) format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".ndjson":
		return jsonl
	case ".json":
		return jsonDoc
	case ".csv":
		return csvFile
	case ".txt", ".text", ".log":
		return lines
	case ".md", ".markdown", ".mdx":
		return markdown
	}
	return document
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
	if f == csvFile {
		return w.csv(r, label)
	}
	unit := UnitRecord
	if f == lines {
		unit = skill.EachLine
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
		if err := w.record(value, where, unit); err != nil {
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
		return w.record(bytes.TrimSpace(value), label, UnitRecord)
	}
	for i, elem := range elems {
		if err := w.record(elem, label+"["+strconv.Itoa(i)+"]", UnitRecord); err != nil {
			return err
		}
	}
	return nil
}

// csv reads CSV with a header row. Each row is a record whose fields are
// named by the header.
func (w *walker) csv(r io.Reader, label string) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		line, _ := cr.FieldPos(0)
		where := label + ":" + strconv.Itoa(line)
		if len(row) != len(header) {
			return fmt.Errorf("%s has %d fields, but the header has %d", where, len(row), len(header))
		}
		// Keep the header's order, which a map would lose.
		var b bytes.Buffer
		b.WriteByte('{')
		for i, name := range header {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(name)
			v, _ := json.Marshal(row[i])
			b.Write(k)
			b.WriteByte(':')
			b.Write(v)
		}
		b.WriteByte('}')
		if err := w.record(b.Bytes(), where, UnitRecord); err != nil {
			return err
		}
	}
}

func (w *walker) record(value json.RawMessage, label, unit string) error {
	state := value
	if w.opts.Field != "" {
		var err error
		if state, err = Lookup(value, w.opts.Field); err != nil {
			return fmt.Errorf("--field %s in %s: %w", w.opts.Field, label, err)
		}
	}
	budget := budget(label)
	if encodedSize(state) <= budget {
		return w.emit(Item{Label: label, Unit: unit, Value: value, State: state})
	}
	// Too large to send whole: cut the record's text, or its JSON laid out
	// one member per line, between lines.
	var text string
	if json.Unmarshal(state, &text) != nil {
		var b bytes.Buffer
		json.Indent(&b, state, "", "  ")
		text = b.String()
	}
	lines := strings.Split(text, "\n")
	pieces := split(lines, 1, len(lines), plainStructure(lines), budget)
	preview, _ := json.Marshal(preview(string(value))) // each part's result keeps a copy
	it := Item{Label: label, Unit: unit, Value: preview}
	for i, p := range pieces {
		s, _ := json.Marshal(partState{Path: label, Part: fmt.Sprintf("%d of %d", i+1, len(pieces)), Content: p.text})
		it.Parts = append(it.Parts, Part{Size: len(p.text), State: s})
	}
	return w.emit(it)
}

// encodedSize is the size of JSON as the client sends it, which escapes
// <, >, and & in strings.
func encodedSize(raw json.RawMessage) int {
	n := len(raw)
	for _, c := range raw {
		if c == '<' || c == '>' || c == '&' {
			n += 5
		}
	}
	return n
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
