package source

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// tok is a token of source code, as the scanners read it.
type tok struct {
	kind  byte // 'p' punctuation or operator, 'k' keyword, 'n' name, 'l' literal, 'c' comment
	val   string
	line  int  // the line it starts on, from 1
	nl    bool // it is the first token on its line
	off   int  // its byte offset in the source
	depth int  // how many brackets are open around it
	open  int  // for a closing bracket, the index of its opening one
}

// lex tokenizes source code with one of chroma's lexers, keeping what the
// scanners need: words, literals, comments, and single punctuation
// characters, with a few operators joined.
//
// Chroma's tokens need some care. It marks characters it did not expect,
// such as the brackets of some TypeScript types, as errors; they are
// still code. Quotes it marks as errors are apostrophes in JSX text. A
// name can hold brackets: it lexes "function f() {" with "f() {" as one
// name. And it splits "=>" and "?." into their characters.
func lex(lexer, src string) ([]tok, error) {
	l := lexers.Get(lexer)
	if l == nil {
		return nil, errLost
	}
	it, err := l.Tokenise(nil, src)
	if err != nil {
		return nil, errLost
	}
	var out []tok
	line, off, nl := 1, 0, true
	add := func(kind byte, val string, at, ln int) {
		out = append(out, tok{kind: kind, val: val, line: ln, nl: nl, off: at})
		nl = false
	}
	for t := it(); t != chroma.EOF; t = it() {
		v, ty, at, ln := t.Value, t.Type, off, line
		off += len(v)
		line += strings.Count(v, "\n")
		literal := ty.InCategory(chroma.LiteralString) || ty.InCategory(chroma.LiteralNumber)
		if n := len(out); literal && n > 0 && out[n-1].kind == 'l' && out[n-1].off+len(out[n-1].val) == at {
			out[n-1].val += v // chroma lexes a string in pieces, some only spaces
			continue
		}
		switch {
		case v == "":
		case literal && ty != chroma.LiteralStringInterpol:
			add('l', v, at, ln)
		case strings.TrimSpace(v) == "":
			nl = nl || strings.Contains(v, "\n")
		case ty == chroma.LiteralStringInterpol:
			// The ${ and } of a template literal, or the braces of an
			// f-string. The code between is lexed as code.
		case ty.InCategory(chroma.Comment):
			add('c', v, at, ln)
			nl = strings.HasSuffix(v, "\n")
		case ty.InCategory(chroma.Operator) && isOperator(v):
			add('p', v, at, ln)
		default:
			// Names, keywords, punctuation, text, and errors: words and
			// single characters.
			word := byte('n')
			if ty.InCategory(chroma.Keyword) {
				word = 'k'
			}
			for i := 0; i < len(v); {
				r, size := utf8.DecodeRuneInString(v[i:])
				switch {
				case r == '\n':
					nl = true
					ln++
				case unicode.IsSpace(r), ty == chroma.Error && (r == '\'' || r == '"' || r == '`'):
				case isWord(r):
					j := i
					for j < len(v) {
						r2, s2 := utf8.DecodeRuneInString(v[j:])
						if !isWord(r2) {
							break
						}
						j += s2
					}
					add(word, v[i:j], at+i, ln)
					i = j
					continue
				default:
					add('p', string(r), at+i, ln)
				}
				i += size
			}
		}
	}
	joined := out[:0]
	for _, t := range out {
		if n := len(joined); n > 0 && !t.nl {
			last := &joined[n-1]
			adjacent := last.off+len(last.val) == t.off
			if adjacent && last.kind == 'p' && t.kind == 'p' {
				switch last.val + t.val {
				case "=>", "->", "?.", "??", "..", "...", "::":
					last.val += t.val
					continue
				}
			}
			if adjacent && last.val == "#" && t.kind == 'n' { // a private name
				last.val, last.kind = "#"+t.val, 'n'
				continue
			}
		}
		joined = append(joined, t)
	}
	return joined, nil
}

func isOperator(v string) bool {
	for _, r := range v {
		if !strings.ContainsRune("=!<>&|+-*/%^~?:.", r) {
			return false
		}
	}
	return true
}

func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

// tokens is a file's code tokens, with its comments set aside.
type tokens struct {
	src  string
	toks []tok
	lead map[int]int // index of a token -> the line where the comments right above it start
}

// prepare sets comments aside and matches brackets. Brackets that do not
// match mean the lexer lost its way, or the code does not parse.
func prepare(src string, all []tok) (*tokens, error) {
	t := &tokens{src: src, lead: map[int]int{}}
	run, runEnd := 0, 0 // the comments above the next token: first and last line
	for _, k := range all {
		if k.kind == 'c' {
			end := k.line + strings.Count(strings.TrimRight(k.val, "\n"), "\n")
			switch {
			case !k.nl:
				run = 0 // after code on its line
			case run == 0 || k.line > runEnd+1:
				run = k.line
			}
			runEnd = end
			continue
		}
		if run > 0 && k.line == runEnd+1 {
			t.lead[len(t.toks)] = run
		}
		run = 0
		t.toks = append(t.toks, k)
	}
	var stack []int
	for i := range t.toks {
		k := &t.toks[i]
		k.depth = len(stack)
		if k.kind != 'p' {
			continue
		}
		switch k.val {
		case "{", "(", "[":
			stack = append(stack, i)
		case "}", ")", "]":
			n := len(stack) - 1
			if n < 0 || t.toks[stack[n]].val != map[string]string{"}": "{", ")": "(", "]": "["}[k.val] {
				return nil, errLost
			}
			k.depth, k.open = n, stack[n]
			stack = stack[:n]
		}
	}
	if len(stack) > 0 {
		return nil, errLost
	}
	return t, nil
}

// close finds the bracket that closes the one at toks[i].
func (t *tokens) close(i int) int {
	d := t.toks[i].depth
	for j := i + 1; j < len(t.toks); j++ {
		if t.toks[j].depth == d && t.toks[j].kind == 'p' && strings.Contains(")]}", t.toks[j].val) {
			return j
		}
	}
	return len(t.toks) - 1
}

// is reports whether toks[i] is the punctuation v.
func (t *tokens) is(i int, v string) bool {
	return i >= 0 && i < len(t.toks) && t.toks[i].kind == 'p' && t.toks[i].val == v
}

// fn makes a unit of toks[s] to toks[e]. Its lead takes in the comments
// right above it.
func (t *tokens) fn(name string, s, e, parent int) fn {
	f := fn{name: name, start: t.toks[s].line, end: t.endLine(e), parent: parent}
	f.lead = f.start
	if l, ok := t.lead[s]; ok {
		f.lead = l
	}
	return f
}

// endLine is the line a token ends on.
func (t *tokens) endLine(i int) int {
	return t.toks[i].line + strings.Count(t.toks[i].val, "\n")
}

// text is the source from toks[s] up to toks[e], not including it, with
// its spaces collapsed.
func (t *tokens) text(s, e int) string {
	if s >= e {
		return ""
	}
	return strings.Join(strings.Fields(t.src[t.toks[s].off:t.toks[e].off]), " ")
}

// dotted skips a name such as "Foo" or "org.junit.Test" at toks[i].
func (t *tokens) dotted(i int) int {
	for i < len(t.toks) && t.toks[i].kind != 'p' && t.toks[i].kind != 'l' {
		i++
		if !t.is(i, ".") {
			break
		}
		i++
	}
	return i
}
