package source

import "strings"

// javaFunctions finds the function units of a Java file: each method and
// constructor of each class, interface, enum, and record, named like
// "User.save" or "Outer.Inner.run", and each field set to a lambda or an
// anonymous class.
func javaFunctions(src string) (code, error) {
	all, err := lex("java", src)
	if err != nil {
		return code{}, err
	}
	t, err := prepare(src, all)
	if err != nil {
		return code{}, err
	}
	p := &javaParser{tokens: t}
	p.body(0, len(t.toks), 0, "", "", 0)
	return p.code, nil
}

type javaParser struct {
	*tokens
	code
}

var javaModifiers = map[string]bool{
	"public": true, "protected": true, "private": true, "static": true, "final": true, "abstract": true,
	"synchronized": true, "native": true, "transient": true, "volatile": true, "strictfp": true,
	"default": true, "sealed": true, "non": true, "-": true,
}

var javaTypes = map[string]bool{"class": true, "interface": true, "enum": true, "record": true}

// end finds the last token of the declaration that starts at toks[s]: a
// semicolon, or the brace that closes its body.
func (p *javaParser) end(s, to, depth int) int {
	hasEq := false
	for i := s; i < to; i++ {
		if p.toks[i].depth != depth {
			continue
		}
		hasEq = hasEq || p.is(i, "=")
		if p.is(i, ";") || p.is(i, "}") && !hasEq {
			return i
		}
	}
	return to - 1
}

// skip skips the annotations and modifiers at toks[s].
func (p *javaParser) skip(s, e int) int {
	for s <= e {
		switch {
		case p.is(s, "@") && p.toks[s+1].val != "interface":
			s = p.dotted(s + 1)
			if p.is(s, "(") {
				s = p.close(s) + 1
			}
		case javaModifiers[p.toks[s].val] && p.toks[s].kind != 'l':
			s++
		default:
			return s
		}
	}
	return s
}

// body reads the declarations from toks[from] up to toks[to]: the top of a
// file, or the body of the type named cls, which is of the given kind.
func (p *javaParser) body(from, to, depth int, cls, kind string, parent int) {
	if kind == "enum" {
		// The constants come first, up to a semicolon.
		for i := from; i < to; i++ {
			if p.toks[i].depth == depth && p.is(i, ";") {
				from = i + 1
				break
			}
			if i == to-1 {
				return
			}
		}
	}
	for s := from; s < to; {
		e := p.end(s, to, depth)
		p.member(s, e, depth, cls, parent)
		s = e + 1
	}
}

func (p *javaParser) member(s, e, depth int, cls string, parent int) {
	k := p.skip(s, e)
	if k > e || p.is(k, "{") || p.is(k, ";") {
		return // an initializer block, or nothing
	}
	t := p.toks[k]
	if depth == 0 && (t.val == "package" || t.val == "import") {
		for l := p.toks[s].line; l <= p.endLine(e); l++ {
			p.imports = append(p.imports, l)
		}
		return
	}
	if p.is(k, "@") { // @interface
		k++
		t = p.toks[k]
	}
	if javaTypes[t.val] && p.toks[k+1].kind != 'p' {
		name := p.toks[k+1].val
		if cls != "" {
			name = cls + "." + name
		}
		for j := k + 1; j <= e; j++ {
			if p.toks[j].depth == depth && p.is(j, "{") {
				p.body(j+1, p.close(j), depth+1, name, t.val, t.line)
				return
			}
		}
		return
	}
	if cls == "" {
		return
	}
	if p.is(k, "<") { // type parameters
		n := 0
		for ; k <= e; k++ {
			n += strings.Count(p.toks[k].val, "<") - strings.Count(p.toks[k].val, ">")
			if n <= 0 && p.toks[k].kind == 'p' {
				k++
				break
			}
		}
	}
	paren, eq := -1, -1
	for j := k; j <= e; j++ {
		if p.toks[j].depth != depth {
			continue
		}
		if paren < 0 && p.is(j, "(") {
			paren = j
		}
		if eq < 0 && p.is(j, "=") {
			eq = j
		}
	}
	short := cls[strings.LastIndex(cls, ".")+1:]
	switch {
	case paren > k && (eq < 0 || paren < eq):
		// A method or constructor, with a body unless it is abstract or
		// an annotation's element with a default.
		if p.is(e, "}") && p.toks[p.toks[e].open-1].val != "default" {
			p.add(cls+"."+p.toks[paren-1].val, s, e, parent)
		}
	case t.val == short && p.is(k+1, "{"):
		p.add(cls+"."+short, s, e, parent) // a record's compact constructor
	case eq > k:
		for j := eq + 1; j < e; j++ {
			if p.is(j, "->") || p.is(j, "{") && p.is(j-1, ")") {
				p.add(cls+"."+p.toks[eq-1].val, s, e, parent)
				return
			}
		}
	}
}

func (p *javaParser) add(name string, s, e, parent int) {
	p.fns = append(p.fns, p.fn(name, s, e, parent))
}
