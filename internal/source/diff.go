package source

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/deepnoodle-ai/decide/internal/template"
)

// Kinds of change to a file in a diff.
const (
	changeAdded    = "added"
	changeModified = "modified"
	changeDeleted  = "deleted"
	changeRenamed  = "renamed"
)

// diffFile is one file in a unified diff.
type diffFile struct {
	oldPath, newPath string
	change           string
	binary           bool
	headers          bool   // the --- and +++ lines were read
	commit           string // the commit the change is in, for git log -p and git format-patch
	name             string // how items name the file, with its commit when the diff has several
	hunks            []hunk
}

// path is the file's name after the change, or before it for a deletion.
func (f *diffFile) path() string {
	if f.change == changeDeleted {
		return f.oldPath
	}
	return f.newPath
}

// hunk is one @@ block. lines keep their first character: ' ' for context,
// '+' for an added line, '-' for a removed one, and '\' for "\ No newline
// at end of file".
type hunk struct {
	header             string // the @@ line
	context            string // the text after the second @@, often the enclosing function
	oldStart, newStart int
	lines              []string
}

// text is the hunk as it appears in the diff, from its @@ line.
func (h hunk) text() string { return h.header + "\n" + strings.Join(h.lines, "\n") }

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// isDiff reports whether text looks like a unified diff, such as the
// output of git diff, git show, git format-patch, or diff -u. It looks at
// the first line and for a file header followed by a hunk.
func isDiff(peek []byte) bool {
	text := string(peek)
	first, _, _ := strings.Cut(strings.TrimLeft(text, "\r\n\t "), "\n")
	starts := false
	for _, p := range []string{"diff ", "--- ", "Index: ", "commit ", "From "} {
		if strings.HasPrefix(first, p) {
			starts = true
		}
	}
	if !starts {
		return false
	}
	lines := strings.Split(text, "\n")
	for i := 0; i+2 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "diff --git ") || isCombined(lines[i]) {
			return true
		}
		if strings.HasPrefix(lines[i], "--- ") && strings.HasPrefix(lines[i+1], "+++ ") && hunkHeader.MatchString(lines[i+2]) {
			return true
		}
	}
	return false
}

// isCombined reports whether line starts a combined diff, which git show
// prints for a merge commit.
func isCombined(line string) bool {
	return strings.HasPrefix(line, "diff --cc ") || strings.HasPrefix(line, "diff --combined ")
}

var commitLine = regexp.MustCompile(`^(?:commit|From) ([0-9a-f]{7,64})\b`)

// parseDiff reads the files and hunks of a unified diff. Text outside
// files and hunks, such as a commit message, is ignored. It also counts
// the combined diffs of merge commits, which it does not read.
func parseDiff(data string) (files []*diffFile, combined int) {
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	var cur *diffFile
	commit := ""
	inCombined := false // in a combined diff, whose lines are skipped
	start := func(f *diffFile) {
		f.commit = commit
		files = append(files, f)
		cur = f
		inCombined = false
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case commitLine.MatchString(line):
			commit = commitLine.FindStringSubmatch(line)[1]
			cur, inCombined = nil, false
		case isCombined(line):
			combined++
			cur, inCombined = nil, true
		case strings.HasPrefix(line, "diff --git "):
			old, new := gitPaths(strings.TrimPrefix(line, "diff --git "))
			start(&diffFile{oldPath: old, newPath: new, change: changeModified})
		case cur != nil && strings.HasPrefix(line, "new file mode"):
			cur.change = changeAdded
		case cur != nil && strings.HasPrefix(line, "deleted file mode"):
			cur.change = changeDeleted
		case cur != nil && strings.HasPrefix(line, "rename from "):
			cur.oldPath, cur.change = unquotePath(strings.TrimPrefix(line, "rename from ")), changeRenamed
		case cur != nil && strings.HasPrefix(line, "rename to "):
			cur.newPath, cur.change = unquotePath(strings.TrimPrefix(line, "rename to ")), changeRenamed
		case cur != nil && (strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch"):
			cur.binary = true
		case inCombined:
		case strings.HasPrefix(line, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			old := headerPath(strings.TrimPrefix(line, "--- "))
			new := headerPath(strings.TrimPrefix(lines[i+1], "+++ "))
			i++
			// git writes a diff --git line first; diff -u starts here.
			if cur == nil || cur.headers {
				start(&diffFile{oldPath: old, newPath: new, change: changeModified})
			}
			switch {
			case old == "/dev/null":
				cur.change = changeAdded
			case new == "/dev/null":
				cur.change = changeDeleted
			}
			if old != "/dev/null" {
				cur.oldPath = old
			}
			if new != "/dev/null" {
				cur.newPath = new
			}
			cur.headers = true
		case cur != nil:
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			h := hunk{header: line, context: strings.TrimSpace(m[5])}
			h.oldStart, _ = strconv.Atoi(m[1])
			h.newStart, _ = strconv.Atoi(m[3])
			oldLeft, newLeft := count(m[2]), count(m[4])
			for i+1 < len(lines) && (oldLeft > 0 || newLeft > 0 || strings.HasPrefix(lines[i+1], `\`)) {
				i++
				l := lines[i]
				switch {
				case l == "":
					// Some tools drop the space of an empty context line.
					l = " "
					oldLeft--
					newLeft--
				case l[0] == ' ':
					oldLeft--
					newLeft--
				case l[0] == '-':
					oldLeft--
				case l[0] == '+':
					newLeft--
				case l[0] == '\\':
				default:
					i-- // the diff ended early
					oldLeft, newLeft = 0, 0
					continue
				}
				h.lines = append(h.lines, l)
			}
			cur.hunks = append(cur.hunks, h)
		}
	}
	return files, combined
}

// count reads a hunk's line count, which is 1 when the diff leaves it out.
func count(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}

// gitPaths reads the paths in a "diff --git a/x b/y" line. It is a fallback
// for files whose diff has no --- and +++ lines, such as a pure rename or a
// binary file. It returns empty paths for a line it cannot read.
func gitPaths(s string) (old, new string) {
	if strings.HasPrefix(s, `"`) {
		if q, err := strconv.QuotedPrefix(s); err == nil {
			old = unquotePath(q)
			return strings.TrimPrefix(old, "a/"), strings.TrimPrefix(unquotePath(strings.TrimSpace(s[len(q):])), "b/")
		}
	}
	// Without quotes, the two paths are usually the same, so split in the
	// middle: "a/x b/x".
	if n, h := len(s), len(s)/2; n%2 == 1 && n >= 7 && s[h:h+3] == " b/" && s[:h] == "a/"+s[h+3:] {
		p := s[2:h]
		return p, p
	}
	if a, b, ok := strings.Cut(s, " b/"); ok && strings.HasPrefix(a, "a/") && len(a) > 2 && b != "" {
		return a[2:], b
	}
	return "", ""
}

// headerPath reads the path in a --- or +++ line, without git's a/ or b/
// and without the timestamp diff -u adds after a tab.
func headerPath(s string) string {
	s, _, _ = strings.Cut(s, "\t")
	s = unquotePath(strings.TrimSpace(s))
	if s == "/dev/null" {
		return s
	}
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}

// unquote reads a path git quoted because it has unusual characters.
func unquotePath(s string) string {
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// lockfiles are generated by package managers and too long to judge.
var lockfiles = []string{
	"go.sum", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb",
	"Cargo.lock", "Gemfile.lock", "poetry.lock", "uv.lock", "Pipfile.lock", "composer.lock", "flake.lock",
}

// skipReason says why a file in a diff is not judged, or "" to judge it.
func (w *walker) skipReason(f *diffFile) string {
	switch {
	case f.binary:
		return "binary file"
	case f.change == changeDeleted:
		return "deleted"
	case slices.Contains(lockfiles, path.Base(f.path())):
		return "lockfile"
	case w.generated(f):
		return "generated"
	case len(f.hunks) == 0 && f.change == changeRenamed:
		return "renamed only"
	case len(f.hunks) == 0:
		return "no changed lines"
	}
	return ""
}

// generatedMarker matches a comment that marks a file as generated, such
// as Go's "// Code generated by protoc. DO NOT EDIT." or "# @generated".
var generatedMarker = regexp.MustCompile(`^\s*(//|#|/\*|\*|<!--|--)\s*(Code generated .* DO NOT EDIT\.|@generated\b)`)

// generated reports whether the new version of the file starts with a
// generated-code marker in its first five lines: in the diff, or else in
// the file on disk, when it is there.
func (w *walker) generated(f *diffFile) bool {
	if f.markedGenerated() {
		return true
	}
	p, ok := w.local(f.newPath)
	if !ok {
		return false
	}
	fh, err := os.Open(p)
	if err != nil {
		return false
	}
	defer fh.Close()
	sc := bufio.NewScanner(io.LimitReader(fh, 4096))
	for n := 0; n < 5 && sc.Scan(); n++ {
		if generatedMarker.MatchString(sc.Text()) {
			return true
		}
	}
	return false
}

// markedGenerated reports whether the diff shows a generated-code marker in
// the new version's first five lines.
func (f *diffFile) markedGenerated() bool {
	for _, h := range f.hunks {
		n := h.newStart
		for _, l := range h.lines {
			if l[0] != '+' && l[0] != ' ' {
				continue
			}
			if n <= 5 && generatedMarker.MatchString(l[1:]) {
				return true
			}
			n++
		}
	}
	return false
}

// diffState is what the model sees of a change.
type diffState struct {
	Path     string `json:"path"`
	OldPath  string `json:"old_path,omitempty"`
	Language string `json:"language,omitempty"`
	Change   string `json:"change"`
	Lines    string `json:"lines,omitempty"` // lines in the new version of the file
	Part     string `json:"part,omitempty"`
	Diff     string `json:"diff"` // the change in unified diff form
}

// diff makes items of a unified diff: each hunk, each changed file, or
// each added line. prefix starts each item's name, for a patch found in a
// folder.
func (w *walker) diff(data []byte, label, prefix string) error {
	each := w.opts.Each
	switch each {
	case "":
		each = template.EachHunk
	case template.EachHunk, template.EachFile, template.EachLine:
	case template.EachFunction:
		return fmt.Errorf("--each function does not read diffs yet; use --each hunk or --each file")
	default:
		return fmt.Errorf("%s is a diff, so --each %s does not apply; use --each hunk, file, or line", label, each)
	}
	w.opts.Changes()
	files, combined := parseDiff(string(data))
	if len(files) == 0 && combined > 0 {
		return fmt.Errorf("%s holds a merge commit's combined diff, which decide does not read; diff against one side instead, such as: git diff main...branch", label)
	}
	if len(files) == 0 {
		return nil // nothing changed
	}
	for _, f := range files {
		if f.path() == "" {
			return fmt.Errorf("%s has a diff --git line without the file's name", label)
		}
	}
	if combined > 0 {
		w.opts.Warn(fmt.Sprintf("Skipped %d combined %s from merge commits", combined, plural(combined, "diff", "diffs")))
	}
	commits := map[string]bool{}
	for _, f := range files {
		commits[f.commit] = true
	}
	for _, f := range files {
		f.name = prefix + f.path()
		if len(commits) > 1 && f.commit != "" {
			f.name += "@" + f.commit[:min(len(f.commit), 7)]
		}
	}
	var skipped []string
	for _, f := range files {
		p := f.path()
		if len(w.opts.Exclude) > 0 && match(w.opts.Exclude, p) {
			continue
		}
		included := len(w.opts.Include) > 0 && match(w.opts.Include, p)
		if len(w.opts.Include) > 0 && !included {
			continue
		}
		// A lockfile or generated file named by --include is judged anyway.
		if why := w.skipReason(f); why != "" && !(included && (why == "lockfile" || why == "generated")) {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", p, why))
			continue
		}
		var err error
		switch each {
		case template.EachHunk:
			err = w.hunks(f)
		case template.EachFile:
			err = w.diffFile(f)
		case template.EachLine:
			err = w.addedLines(f)
		}
		if err != nil {
			return err
		}
	}
	if n := len(skipped); n > 0 {
		names := strings.Join(skipped[:min(n, 3)], ", ")
		if n > 3 {
			names += fmt.Sprintf(", and %d more", n-3)
		}
		w.opts.Warn("Skipped " + names)
	}
	return nil
}

func (f *diffFile) state(lines, part, diff string) diffState {
	s := diffState{Path: f.path(), Language: strings.TrimPrefix(path.Ext(f.path()), "."), Change: f.change,
		Lines: lines, Part: part, Diff: diff}
	if f.change == changeRenamed {
		s.OldPath = f.oldPath
	}
	return s
}

// newLines says which lines of the new file a hunk covers, such as "40-52".
func (h hunk) newLines() string {
	n := 0
	for _, l := range h.lines {
		if l[0] == ' ' || l[0] == '+' {
			n++
		}
	}
	if n <= 1 {
		return strconv.Itoa(h.newStart)
	}
	return fmt.Sprintf("%d-%d", h.newStart, h.newStart+n-1)
}

// firstChange is the line number, in the new file, of a hunk's first added
// or removed line, so an item's name points at the change rather than at
// the context above it.
func (h hunk) firstChange() int {
	n := h.newStart
	for _, l := range h.lines {
		switch l[0] {
		case '+', '-':
			return max(n, 1)
		case ' ':
			n++
		}
	}
	return max(h.newStart, 1)
}

func (w *walker) hunks(f *diffFile) error {
	for _, h := range f.hunks {
		label := fmt.Sprintf("%s:%d", f.name, h.firstChange())
		value, _ := json.Marshal(h.context)
		if h.context == "" {
			value = nil
		}
		it := Item{Label: label, Unit: template.EachHunk, Value: value, Diff: true}
		if err := w.emitDiff(&it, f, h.newLines(), append([]string{h.header}, h.lines...)); err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) diffFile(f *diffFile) error {
	var lines []string
	for _, h := range f.hunks {
		lines = append(lines, h.header)
		lines = append(lines, h.lines...)
	}
	it := Item{Label: f.name, Unit: template.EachFile, Diff: true}
	return w.emitDiff(&it, f, "", lines)
}

// emitDiff sets an item's state to the diff lines, cut into parts at hunks,
// then between lines, when they are too large for one request.
func (w *walker) emitDiff(it *Item, f *diffFile, where string, lines []string) error {
	text := strings.Join(lines, "\n")
	budget := budget(f.path())
	if jsonSize(text) <= budget {
		it.State, _ = json.Marshal(f.state(where, "", text))
		return w.emit(*it)
	}
	st := structure{breaks: map[int]bool{}, section: func(int, int) string { return "" }}
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			st.breaks[i+1] = true
		}
	}
	parts := split(lines, 1, len(lines), st, budget)
	for i, p := range parts {
		state, _ := json.Marshal(f.state(where, fmt.Sprintf("%d of %d", i+1, len(parts)), p.text))
		it.Parts = append(it.Parts, Part{Size: len(p.text), State: state})
	}
	return w.emit(*it)
}

func (w *walker) addedLines(f *diffFile) error {
	for _, h := range f.hunks {
		n := h.newStart
		for _, l := range h.lines {
			switch l[0] {
			case ' ':
				n++
			case '+':
				text := strings.TrimSpace(l[1:])
				if text != "" {
					value, _ := json.Marshal(text)
					state, _ := json.Marshal(f.state(strconv.Itoa(n), "", l))
					if err := w.emit(Item{Label: fmt.Sprintf("%s:%d", f.name, n), Unit: template.EachLine, Value: value, State: state}); err != nil {
						return err
					}
				}
				n++
			}
		}
	}
	return nil
}

// local finds a file that a diff names, in the working directory or at the
// root of its git repository. The path comes from the diff, so the file it
// leads to, after following any symbolic links, must be inside them.
func (w *walker) local(name string) (string, bool) {
	rel := path.Clean(name)
	if rel == "" || rel == "." || path.IsAbs(rel) || filepath.IsAbs(filepath.FromSlash(rel)) || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	for _, dir := range []string{".", w.repoRoot()} {
		if dir == "" {
			continue
		}
		root, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || !within(root, real) {
			continue
		}
		if info, err := os.Stat(real); err == nil && info.Mode().IsRegular() {
			return real, true
		}
	}
	return "", false
}

// within reports whether path p is inside folder dir.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// repoRoot finds the top of the git repository holding the working
// directory, or returns "".
func (w *walker) repoRoot() string {
	if w.root != nil {
		return *w.root
	}
	root := ""
	if dir, err := os.Getwd(); err == nil {
		for {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				root = dir
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	w.root = &root
	return root
}
