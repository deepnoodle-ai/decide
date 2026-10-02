package source

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// mdBlock is one block of a Markdown file: a heading, text (a paragraph,
// list item, quote, or table), code, HTML, a horizontal rule, or front
// matter.
type mdBlock struct {
	kind       string   // "heading", "text", "code", "html", "rule", or "meta"
	start, end int      // line numbers, from 1, inclusive
	headings   []string // the headings above the block; a heading includes itself
	level      int      // a heading's level, from 1 to 6
}

const (
	mdHeading = "heading"
	mdText    = "text"
	mdCode    = "code"
	mdHTML    = "html"
	mdRule    = "rule"
	mdMeta    = "meta"
)

var (
	atxPattern      = regexp.MustCompile(`^ {0,3}(#{1,6})(?:\s+(.*?))?(?:\s+#+)?\s*$`)
	listItemPattern = regexp.MustCompile(`^ {0,3}([-*+]|[0-9]{1,9}[.)])(\s|$)`)
	rulePattern     = regexp.MustCompile(`^ {0,3}((- *){3,}|(\* *){3,}|(_ *){3,})$`)
	fencePattern    = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	htmlPattern     = regexp.MustCompile(`^ {0,3}<(/?[A-Za-z][A-Za-z0-9-]*[\s/>]|/?[A-Za-z][A-Za-z0-9-]*$|!--)`)
)

// parseMarkdown splits Markdown into blocks. It follows the CommonMark
// rules that matter for telling blocks apart, and is lenient elsewhere.
func parseMarkdown(lines []string) []mdBlock {
	var (
		out   []mdBlock
		cur   *mdBlock // the open text block
		list  bool     // the open text block is a list item
		stack []heading
	)
	path := func() []string {
		titles := make([]string, len(stack))
		for i, h := range stack {
			titles[i] = h.title
		}
		return titles
	}
	closeText := func() {
		if cur != nil {
			out = append(out, *cur)
		}
		cur, list = nil, false
	}
	add := func(kind string, start, end int) {
		out = append(out, mdBlock{kind: kind, start: start, end: end, headings: path()})
	}
	addHeading := func(level int, title string, start, end int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, heading{level, title})
		out = append(out, mdBlock{kind: mdHeading, start: start, end: end, headings: path(), level: level})
	}
	// fenceEnd returns the index of the line that closes the fence opened
	// on line i, or the last line when it never closes.
	fenceEnd := func(i int, fence string) int {
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				return j
			}
		}
		return len(lines) - 1
	}
	indented := func(s string) bool { return strings.HasPrefix(s, "  ") || strings.HasPrefix(s, "\t") }

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		n := i + 1
		switch {
		case i == 0 && trimmed == "---":
			end := -1
			for j := 1; j < len(lines); j++ {
				if t := strings.TrimSpace(lines[j]); t == "---" || t == "..." {
					end = j
					break
				}
			}
			if end < 0 {
				add(mdRule, n, n) // no closing line, so not front matter
				continue
			}
			add(mdMeta, n, end+1)
			i = end

		case list && trimmed != "" && indented(line):
			// A line indented under a list item belongs to it, including a
			// nested list or a fenced code block.
			if m := fencePattern.FindStringSubmatch(trimmed); m != nil {
				i = fenceEnd(i, m[1])
			}
			cur.end = i + 1

		case trimmed == "":
			if list && i+1 < len(lines) && indented(lines[i+1]) && strings.TrimSpace(lines[i+1]) != "" {
				continue // a blank line inside a list item
			}
			closeText()

		case fencePattern.MatchString(line):
			closeText()
			end := fenceEnd(i, fencePattern.FindStringSubmatch(line)[1])
			add(mdCode, n, end+1)
			i = end

		case atxPattern.MatchString(line):
			closeText()
			m := atxPattern.FindStringSubmatch(line)
			addHeading(len(m[1]), strings.TrimSpace(m[2]), n, n)

		case cur != nil && !list && strings.Trim(trimmed, "=") == "" && strings.HasPrefix(trimmed, "="),
			cur != nil && !list && strings.Trim(trimmed, "-") == "" && strings.HasPrefix(trimmed, "-"):
			// The open paragraph is a heading underlined with = or -.
			level := 1
			if trimmed[0] == '-' {
				level = 2
			}
			title := strings.Join(trimAll(lines[cur.start-1:cur.end]), " ")
			start := cur.start
			cur = nil
			addHeading(level, title, start, n)

		case rulePattern.MatchString(line):
			closeText()
			add(mdRule, n, n)

		case cur == nil && htmlPattern.MatchString(line):
			end := i
			for end+1 < len(lines) && strings.TrimSpace(lines[end+1]) != "" {
				end++
			}
			if strings.HasPrefix(trimmed, "<!--") {
				for end < len(lines)-1 && !strings.Contains(strings.Join(lines[i:end+1], "\n"), "-->") {
					end++
				}
			}
			add(mdHTML, n, end+1)
			i = end

		case cur == nil && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")):
			end := i
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				if !strings.HasPrefix(lines[j], "    ") && !strings.HasPrefix(lines[j], "\t") {
					break
				}
				end = j
			}
			add(mdCode, n, end+1)
			i = end

		case listItemPattern.MatchString(line):
			closeText()
			cur = &mdBlock{kind: mdText, start: n, end: n, headings: path()}
			list = true

		default:
			if cur == nil {
				cur = &mdBlock{kind: mdText, start: n, end: n, headings: path()}
			}
			cur.end = n
		}
	}
	closeText()
	return out
}

type heading struct {
	level int
	title string
}

func trimAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSpace(l)
	}
	return out
}

// piece is a span of a text file that becomes one item or one part.
type piece struct {
	start, end int    // line numbers, from 1, inclusive
	anchor     string // for a section, its GitHub-style anchor
	section    string // the headings above it, such as "Install › macOS"
	text       string
}

func (p piece) lines() string {
	if p.start == p.end {
		return strconv.Itoa(p.start)
	}
	return strconv.Itoa(p.start) + "-" + strconv.Itoa(p.end)
}

func span(lines []string, start, end int) string {
	return strings.TrimRight(strings.Join(lines[start-1:end], "\n"), " \t\n")
}

// sectionPath joins heading titles for the model to read.
func sectionPath(headings []string) string { return strings.Join(headings, " › ") }

// markdownParagraphs returns each paragraph, list item, quote, and table,
// with the headings above it. Headings, code, HTML, and front matter are
// not paragraphs.
func markdownParagraphs(lines []string) []piece {
	var out []piece
	for _, b := range parseMarkdown(lines) {
		if b.kind == mdText {
			out = append(out, piece{start: b.start, end: b.end, section: sectionPath(b.headings), text: span(lines, b.start, b.end)})
		}
	}
	return out
}

// markdownSections returns the text under each heading, up to the next
// heading of any level, with the heading path as its section. A heading
// with nothing under it before the next heading is not a section; its
// title is part of the next one's path. Text before the first heading is
// a section without a heading.
func markdownSections(lines []string) []piece {
	var out []piece
	slugs := map[string]int{}
	var cur *piece
	flush := func() {
		if cur != nil && cur.end >= cur.start {
			cur.text = span(lines, cur.start, cur.end)
			if strings.TrimSpace(cur.text) != "" {
				out = append(out, *cur)
			}
		}
		cur = nil
	}
	for _, b := range parseMarkdown(lines) {
		switch b.kind {
		case mdHeading:
			flush()
			title := b.headings[len(b.headings)-1]
			cur = &piece{start: b.end + 1, end: b.end, anchor: slug(title, slugs), section: sectionPath(b.headings)}
		case mdMeta, mdRule:
		default:
			if cur == nil {
				cur = &piece{start: b.start, end: b.end}
			}
			if cur.end < cur.start {
				cur.start = b.start
			}
			cur.end = b.end
		}
	}
	flush()
	return out
}

// slug makes the anchor GitHub gives a heading: lowercase, without
// punctuation, with hyphens for spaces, and numbered when repeated.
func slug(title string, seen map[string]int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	base := b.String()
	s := base
	for n := 1; seen[s] > 0; n++ {
		s = base + "-" + strconv.Itoa(n) // "intro-1" may itself be a heading
	}
	seen[s]++
	return s
}

// plainParagraphs returns the runs of lines between blank lines.
func plainParagraphs(lines []string) []piece {
	var out []piece
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i == len(lines) || strings.TrimSpace(lines[i]) == "" {
			if start > 0 {
				out = append(out, piece{start: start, end: i, text: span(lines, start, i)})
				start = 0
			}
			continue
		}
		if start == 0 {
			start = i + 1
		}
	}
	return out
}

// structure is where a file may be cut and what to call each part: the
// lines a part may start on, and the section a line falls in.
type structure struct {
	breaks  map[int]bool
	section func(start, end int) string // what lines start to end are part of
	within  *structure                  // where to cut a span between breaks that is too large; nil to cut between lines
}

// markdownStructure breaks a Markdown file between blocks, and names each
// part by the headings above it.
func markdownStructure(lines []string) structure {
	blocks := parseMarkdown(lines)
	s := structure{breaks: map[int]bool{}}
	for _, b := range blocks {
		s.breaks[b.start] = true
	}
	s.section = func(line, _ int) string {
		var path []string
		for _, b := range blocks {
			if b.start > line {
				break
			}
			path = b.headings
		}
		return sectionPath(path)
	}
	return s
}

// plainStructure breaks text after blank lines.
func plainStructure(lines []string) structure {
	s := structure{breaks: map[int]bool{}, section: func(int, int) string { return "" }}
	for i := 2; i <= len(lines); i++ {
		if strings.TrimSpace(lines[i-2]) == "" {
			s.breaks[i] = true
		}
	}
	return s
}

// jsonSize is the size of s inside a JSON string as the client sends it,
// with quotes, backslashes, control characters, and <, >, and & escaped.
func jsonSize(s string) int {
	n := len(s)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t':
			n++
		case c < 0x20 || c == '<' || c == '>' || c == '&':
			n += 5
		}
	}
	return n
}

// split cuts lines start to end into parts whose text is at most budget
// bytes once encoded as JSON. It breaks where the structure allows when it
// can, then between lines, and cuts a line only when one line is over the
// budget.
func split(lines []string, start, end int, st structure, budget int) []piece {
	breaks, section := st.breaks, st.section

	// Segments run from one break to the next.
	type segment struct{ start, end, size int }
	var segs []segment
	for i := start; i <= end; i++ {
		size := jsonSize(lines[i-1]) + 2 // and its \n
		if len(segs) == 0 || breaks[i] {
			segs = append(segs, segment{i, i, size})
			continue
		}
		segs[len(segs)-1].end = i
		segs[len(segs)-1].size += size
	}
	var out []piece
	var cur *segment
	emit := func() {
		if cur != nil && strings.TrimSpace(span(lines, cur.start, cur.end)) != "" {
			for strings.TrimSpace(lines[cur.end-1]) == "" {
				cur.end--
			}
			out = append(out, piece{start: cur.start, end: cur.end, section: section(cur.start, cur.end), text: span(lines, cur.start, cur.end)})
		}
		cur = nil
	}
	for _, s := range segs {
		if cur != nil && cur.size+s.size <= budget {
			cur.end, cur.size = s.end, cur.size+s.size
			continue
		}
		emit()
		if s.size <= budget {
			c := s
			cur = &c
			continue
		}
		if st.within != nil {
			out = append(out, split(lines, s.start, s.end, *st.within, budget)...)
			continue
		}
		// A segment over the budget is cut between lines, and a line over
		// the budget into pieces.
		for i := s.start; i <= s.end; i++ {
			size := jsonSize(lines[i-1]) + 2
			if cur != nil && cur.size+size <= budget {
				cur.end, cur.size = i, cur.size+size
				continue
			}
			emit()
			if size <= budget {
				cur = &segment{i, i, size}
				continue
			}
			for _, chunk := range chunks(lines[i-1], budget) {
				out = append(out, piece{start: i, end: i, section: section(i, i), text: chunk})
			}
		}
	}
	emit()
	return out
}

// chunks cuts a long line into pieces of at most n bytes once encoded as
// JSON, between runes.
func chunks(s string, n int) []string {
	var out []string
	start, size := 0, 0
	for i, r := range s {
		rs := jsonSize(string(r))
		if size+rs > n && i > start {
			out = append(out, s[start:i])
			start, size = i, 0
		}
		size += rs
	}
	return append(out, s[start:])
}
