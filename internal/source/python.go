package source

// pyFunctions finds the function units of a Python file: each top-level
// function, and each method of a top-level class or a class within it,
// named like "User.save". A unit starts at its decorators. Code inside a
// function is part of it.
func pyFunctions(src string) (code, error) {
	all, err := lex("python", src)
	if err != nil {
		return code{}, err
	}
	t, err := prepare(src, all)
	if err != nil {
		return code{}, err
	}
	// Logical lines start a statement: the first token on a line, outside
	// brackets, and not after a backslash.
	lineStart := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lineStart = append(lineStart, i+1)
		}
	}
	var lines []pyLine
	for i, k := range t.toks {
		closing := t.is(i, ")") || t.is(i, "]") || t.is(i, "}")
		if k.nl && k.depth == 0 && !closing && !t.is(i-1, `\`) {
			lines = append(lines, pyLine{tok: i, indent: k.off - lineStart[k.line-1]})
		}
	}
	p := &pyParser{tokens: t, lines: lines}
	p.block(0, len(lines), "", 0)
	return p.code, nil
}

// compound are the statements whose blocks can define functions.
var compound = map[string]bool{
	"if": true, "elif": true, "else": true, "try": true, "except": true, "finally": true,
	"with": true, "for": true, "while": true, "async": true,
}

type pyLine struct {
	tok    int // the index of its first token
	indent int
}

type pyParser struct {
	*tokens
	code
	lines []pyLine
}

// block reads the statements of logical lines a up to b, which all have
// the same indent. In a class, prefix names it, such as "User.", and
// parent is the line it starts on.
func (p *pyParser) block(a, b int, prefix string, parent int) {
	decorated := -1
	for i := a; i < b; {
		l := p.lines[i]
		j := i + 1
		for j < b && p.lines[j].indent > l.indent {
			j++
		}
		last := len(p.toks) - 1
		if j < len(p.lines) {
			last = p.lines[j].tok - 1
		}
		k := l.tok
		start := k
		if decorated >= 0 {
			start = decorated
		}
		switch first := p.toks[k]; {
		case p.is(k, "@"):
			if decorated < 0 {
				decorated = k
			}
			i = j
			continue
		case first.val == "async" && p.toks[k+1].val == "def", first.val == "def":
			if first.val == "async" {
				k++
			}
			p.fns = append(p.fns, p.fn(prefix+p.toks[k+1].val, start, last, parent))
		case first.val == "class":
			p.block(i+1, j, prefix+p.toks[k+1].val+".", first.line)
		case first.kind == 'k' && compound[first.val]:
			// Functions defined under if TYPE_CHECKING:, in a try, and so on.
			p.block(i+1, j, prefix, parent)
		case prefix == "" && (first.val == "import" || first.val == "from") && first.kind == 'k':
			for n := first.line; n <= p.endLine(last); n++ {
				p.imports = append(p.imports, n)
			}
		}
		decorated = -1
		i = j
	}
}
