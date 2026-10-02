package source

import (
	"encoding/json"
	"strings"
)

// jsFunctions finds the function units of a JavaScript or TypeScript file:
//
//   - each top-level function;
//   - each method, constructor, accessor, and function-valued property of
//     a top-level class, named like "User.save";
//   - each other top-level statement that holds a function, such as
//     "const handler = async (req) => {...}", "module.exports = ...", or
//     `app.get("/users", ...)`;
//   - in a test file, each test: a describe block is read like a class,
//     so each it, test, or hook inside it is a unit, named like
//     `parser › it "reads a header"`.
//
// Overloads, types, interfaces, enums, imports, and constants are not
// units, and neither is code inside a function.
func jsFunctions(src string) (code, error) {
	all, err := lex("tsx", src)
	if err != nil {
		return code{}, err
	}
	t, err := prepare(src, all)
	if err != nil {
		return code{}, err
	}
	p := &jsParser{tokens: t}
	p.statements(0, len(t.toks), 0, "", 0)
	return p.code, nil
}

type jsParser struct {
	*tokens
	code
}

// statements reads the statements from toks[from] up to toks[to], all at
// one depth. Inside a describe block, prefix names the block, such as
// "parser › ", and parent is the line it starts on.
func (p *jsParser) statements(from, to, depth int, prefix string, parent int) {
	for s := from; s < to; {
		e := p.end(s, to, depth, false)
		p.statement(s, e, depth, prefix, parent)
		s = e + 1
	}
}

// members reads the members of a class body.
func (p *jsParser) members(from, to, depth int, cls string, parent int) {
	for s := from; s < to; {
		e := p.end(s, to, depth, true)
		p.member(s, e, depth, cls, parent)
		s = e + 1
	}
}

// continues are tokens that, starting a line, continue the statement on
// the line before.
var continues = map[string]bool{
	".": true, "?.": true, "(": true, "[": true, ",": true, "?": true, ":": true, "=>": true,
	"=": true, "+": true, "-": true, "*": true, "/": true, "%": true, "&&": true, "||": true, "??": true,
	"&": true, "|": true, "^": true, "<": true, ">": true, "==": true, "===": true, "!=": true, "!==": true,
	"<=": true, ">=": true, "+=": true, "-=": true, "instanceof": true, "in": true, "as": true, "satisfies": true,
	"else": true, "catch": true, "finally": true, "extends": true, "implements": true, ")": true, "]": true, "}": true,
	"**": true, "<<": true, ">>": true, ">>>": true, "??=": true, "||=": true, "&&=": true,
}

// starts are keywords that start a statement, so a line that begins with
// one ends the statement before it.
var starts = map[string]bool{
	"const": true, "let": true, "var": true, "function": true, "class": true, "export": true,
	"import": true, "interface": true, "enum": true, "declare": true, "abstract": true,
}

// end finds the last token of the statement or class member that starts
// at toks[s]. Without semicolons, a statement ends at a line break where
// the next line cannot continue it.
func (p *jsParser) end(s, to, depth int, member bool) int {
	decorators := p.is(s, "@")
	hasEq := false
	for i := s; i < to; i++ {
		t := p.toks[i]
		if t.depth != depth {
			continue
		}
		if p.is(i, "=") {
			hasEq = true
		}
		if p.is(i, ";") || i+1 >= to {
			return i
		}
		next := p.toks[i+1]
		inDecorators := decorators
		if decorators && next.depth == depth && next.kind != 'p' && (p.is(i, ")") || t.kind == 'n') {
			decorators = false // past the decorators, at a keyword or name
		}
		if p.is(i, "}") && !hasEq && next.depth == depth && !p.typeBrace(t.open) {
			// A block that ends a declaration: always, for a method.
			if member || !p.continues(i+1) && p.blockEnds(s) {
				return i
			}
		}
		if next.nl && next.depth == depth && !inDecorators && !p.continues(i+1) &&
			(p.canEnd(i) || next.kind == 'k' && starts[next.val]) {
			if p.is(i+1, "{") && !p.is(i, "}") {
				continue // a brace on its own line
			}
			return i
		}
	}
	return to - 1
}

func (p *jsParser) continues(i int) bool {
	t := p.toks[i]
	return (t.kind == 'p' || t.kind == 'k' || t.kind == 'n') && continues[t.val]
}

// canEnd reports whether a statement can end with toks[i].
func (p *jsParser) canEnd(i int) bool {
	t := p.toks[i]
	switch t.kind {
	case 'n', 'l':
		return true
	case 'p':
		switch t.val {
		case ")", "]", "}", "++", "--", "!":
			return true
		}
		return false
	}
	switch t.val {
	case "this", "true", "false", "null", "undefined", "super", "void", "never", "any", "unknown",
		"string", "number", "boolean", "object", "symbol", "bigint":
		return true
	}
	return false
}

// typeBrace reports whether the brace at toks[i] opens an object type or
// literal rather than a block, judging by what comes before it.
func (p *jsParser) typeBrace(i int) bool {
	if i == 0 {
		return false
	}
	switch p.toks[i-1].val {
	case ":", "|", "&", "<", ",", "=", "(", "[", "?", "=>", "extends", "keyof", "typeof", "return":
		return p.toks[i-1].kind != 'l'
	}
	return false
}

// blockEnds reports whether a statement ending in a block ends there: a
// function, class, or control statement, rather than an expression such
// as an object literal.
func (p *jsParser) blockEnds(s int) bool {
	k := p.skipModifiers(s)
	if k >= len(p.toks) {
		return false
	}
	switch p.toks[k].val {
	case "function", "class", "if", "for", "while", "do", "try", "switch", "interface", "enum", "namespace", "module", "declare", "abstract":
		return true
	case "async":
		return k+1 < len(p.toks) && p.toks[k+1].val == "function"
	}
	return false
}

var jsModifiers = map[string]bool{"export": true, "default": true, "declare": true, "abstract": true}

// skipModifiers skips the decorators and modifiers at toks[s].
func (p *jsParser) skipModifiers(s int) int {
	for s < len(p.toks) {
		t := p.toks[s]
		switch {
		case p.is(s, "@"):
			s = p.dotted(s + 1)
			if p.is(s, "(") {
				s = p.close(s) + 1
			}
		case t.kind != 'l' && t.kind != 'p' && jsModifiers[t.val]:
			s++
		default:
			return s
		}
	}
	return s
}

// hasFunction reports whether toks[from:to] hold a function: an arrow,
// the function keyword, or a method's body.
func (p *jsParser) hasFunction(from, to int) bool {
	for i := from; i < to; i++ {
		t := p.toks[i]
		if p.is(i, "=>") || t.kind == 'k' && t.val == "function" || p.is(i, "{") && p.is(i-1, ")") && i > from {
			return true
		}
	}
	return false
}

// assignment finds the = of a statement at its depth, or returns -1.
func (p *jsParser) assignment(s, e, depth int) int {
	for i := s; i <= e; i++ {
		if p.toks[i].depth == depth && p.is(i, "=") {
			return i
		}
	}
	return -1
}

func (p *jsParser) add(name string, s, e, parent int) {
	p.fns = append(p.fns, p.fn(name, s, e, parent))
}

func (p *jsParser) statement(s, e, depth int, prefix string, parent int) {
	k := p.skipModifiers(s)
	if k > e {
		return
	}
	t := p.toks[k]
	exported := false
	for i := s; i < k; i++ {
		exported = exported || p.toks[i].val == "default"
	}
	if t.kind == 'k' || t.kind == 'n' {
		switch t.val {
		case "for", "if", "while", "do", "switch", "try", "with":
			return
		case "import":
			if depth == 0 && !p.is(k+1, "(") {
				p.importLines(s, e)
				return
			}
		case "namespace", "module":
			// A namespace holds functions; a module "name" only declarations.
			if n := p.toks[k+1]; n.kind == 'n' || n.kind == 'k' {
				for j := k + 1; j <= e; j++ {
					if p.toks[j].depth == depth && p.is(j, "{") {
						p.statements(j+1, p.close(j), depth+1, prefix+p.text(k+1, j)+".", t.line)
						return
					}
				}
			}
			if !p.is(k+1, ".") && !p.is(k+1, "=") && !p.is(k+1, "(") {
				return
			}
		case "interface", "enum", "type":
			// A declaration, unless the word is a name: "module.exports".
			if !p.is(k+1, ".") && !p.is(k+1, "=") && !p.is(k+1, "(") {
				return
			}
		case "async", "function":
			if t.val == "async" {
				k++
			}
			if k <= e && p.toks[k].val == "function" {
				if !p.is(e, "}") {
					return // an overload, or a declaration without a body
				}
				name := "default"
				for j := k + 1; j <= e && j < k+3; j++ {
					if p.toks[j].kind == 'n' || p.toks[j].kind == 'k' {
						name = p.toks[j].val
						break
					}
				}
				p.add(prefix+name, s, e, parent)
				return
			}
		case "class":
			name := "default"
			if n := p.toks[k+1]; (n.kind == 'n' || n.kind == 'k') && n.val != "extends" && n.val != "implements" {
				name = n.val
			}
			for j := k + 1; j <= e; j++ {
				if p.toks[j].depth == depth && p.is(j, "{") && !p.typeBrace(j) {
					p.members(j+1, p.close(j), depth+1, prefix+name, p.toks[k].line)
					return
				}
			}
			return
		case "const", "let", "var":
			a := p.assignment(k, e, depth)
			if a < 0 {
				return
			}
			if depth == 0 && p.requires(a+1, e) {
				p.importLines(s, e)
				return
			}
			if !p.hasFunction(a+1, e+1) {
				return
			}
			name := ""
			if p.is(k+1, "{") || p.is(k+1, "[") {
				name = p.text(k+1, a)
			} else if n := p.toks[k+1]; n.kind == 'n' || n.kind == 'k' {
				name = n.val
			}
			p.add(prefix+name, s, e, parent)
			return
		}
	}
	if exported {
		if p.hasFunction(k, e+1) {
			p.add(prefix+"default", s, e, parent)
		}
		return
	}
	if p.toks[s].val == "export" || !p.hasFunction(k, e+1) {
		return // export { ... }, export * from, or no function
	}
	if a := p.assignment(k, e, depth); a >= 0 {
		var b strings.Builder
		for j := k; j < a; j++ {
			b.WriteString(p.toks[j].val)
		}
		p.add(prefix+b.String(), s, e, parent)
		return
	}
	// A call, named by its text up to the first parenthesis and its first
	// argument when that is a string, such as `it "works"`.
	name, call, label := "statement", -1, ""
	for j := k; j <= e; j++ {
		if p.is(j, "(") {
			call = j
			if text := p.text(k, j); text != "" {
				name = text
			}
			// it.each(table)("name", ...) takes its name from the last call.
			for args := j; p.toks[args+1].kind != 'l' && p.is(p.close(args)+1, "("); {
				args = p.close(args) + 1
				if lit := p.toks[args+1]; lit.kind == 'l' {
					label = unquote(lit.val)
					name += " " + quote(label)
					break
				}
			}
			if lit := p.toks[j+1]; label == "" && lit.kind == 'l' && lit.depth == p.toks[j].depth+1 {
				label = unquote(lit.val)
				if label != "" || len(lit.val) >= 2 {
					name += " " + quote(label)
				}
			}
			break
		}
	}
	if call >= 0 && describes[p.text(k, call)] {
		if body := p.callback(call); body >= 0 {
			before := len(p.fns)
			inner := label
			if inner == "" {
				inner = p.text(k, call)
			}
			p.statements(body+1, p.close(body), p.toks[body].depth+1, prefix+inner+" › ", p.toks[s].line)
			if len(p.fns) > before {
				return
			}
		}
	}
	p.add(prefix+name, s, e, parent)
}

// describes are the calls that group tests.
var describes = map[string]bool{
	"describe": true, "describe.only": true, "describe.skip": true, "describe.concurrent": true,
	"describe.sequential": true, "context": true, "suite": true, "fdescribe": true, "xdescribe": true,
}

// callback finds the body of the last function passed to the call whose
// parenthesis is at toks[call], or returns -1.
func (p *jsParser) callback(call int) int {
	end := p.close(call)
	body := -1
	for j := call + 1; j < end; j++ {
		if p.toks[j].depth == p.toks[call].depth+1 && p.is(j, "{") && (p.is(j-1, "=>") || p.is(j-1, ")")) {
			body = j
		}
	}
	return body
}

// requires reports whether an initializer is a require call: an import.
func (p *jsParser) requires(from, to int) bool {
	return from <= to && p.toks[from].val == "require" && p.is(from+1, "(")
}

func (p *jsParser) importLines(s, e int) {
	for l := p.toks[s].line; l <= p.endLine(e); l++ {
		p.imports = append(p.imports, l)
	}
}

var jsMemberModifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "static": true, "readonly": true, "abstract": true,
	"override": true, "async": true, "declare": true, "accessor": true, "*": true,
}

// member records a class member if it is a method, or a property set to a
// function.
func (p *jsParser) member(s, e, depth int, cls string, parent int) {
	k := p.skipModifiers(s)
	for k < e {
		v := p.toks[k].val
		accessor := (v == "get" || v == "set") && !p.is(k+1, "(") && !p.is(k+1, "=") && !p.is(k+1, ":") && !p.is(k+1, ";")
		if !jsMemberModifiers[v] && !accessor {
			break
		}
		k++
	}
	if k > e || p.is(k, "{") {
		return // nothing, or a static block
	}
	name, next := p.toks[k].val, k+1
	if p.is(k, "[") {
		c := p.close(k)
		name = strings.ReplaceAll(p.text(k, c), " ", "") + "]"
		next = c + 1
	} else if p.toks[k].kind == 'l' {
		name = unquote(p.toks[k].val)
	}
	for next <= e && (p.is(next, "?") || p.is(next, "!")) {
		next++
	}
	if next > e {
		return
	}
	if a := p.assignment(k, e, depth); a >= 0 && (p.is(next, "=") || p.is(next, ":")) {
		if p.hasFunction(a+1, e+1) {
			p.add(cls+"."+name, s, e, parent)
		}
		return
	}
	if (p.is(next, "(") || p.is(next, "<")) && p.is(e, "}") {
		p.add(cls+"."+name, s, e, parent)
	}
}

// unquote returns the text of a string literal, or "" when it is not one.
func unquote(lit string) string {
	if len(lit) < 2 || !strings.ContainsAny(lit[:1], `'"`+"`") || lit[len(lit)-1] != lit[0] {
		return ""
	}
	return strings.NewReplacer(`\"`, `"`, `\'`, `'`, "\\`", "`", `\\`, `\`).Replace(lit[1 : len(lit)-1])
}

// quote quotes a name for a label, as JSON does but without escaping <, >,
// and &.
func quote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSpace(b.String())
}
