package source

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
)

// A function unit: a function, or a method of a class or type, found in a
// source file.
type fn struct {
	name       string // such as "parse" or "User.save"
	start, end int    // where its declaration starts and ends, from 1, inclusive
	lead       int    // where it starts with the comments right above it
	parent     int    // the line that starts its class or type, or 0
}

// code is what a scanner finds in a source file.
type code struct {
	fns     []fn
	imports []int // lines of the file's package and import statements
}

// errLost means a scanner could not follow a file's structure, so the
// file is judged whole instead.
var errLost = errors.New("lost track of the structure")

// language is a programming language whose functions can be found.
type language struct {
	name string
	exts []string
	scan func(src string) (code, error)
}

var languages = []language{
	{"Go", []string{".go"}, goFunctions},
	{"Python", []string{".py", ".pyi"}, pyFunctions},
	{"JavaScript", []string{".js", ".jsx", ".mjs", ".cjs"}, jsFunctions},
	{"TypeScript", []string{".ts", ".tsx", ".mts", ".cts"}, jsFunctions},
	{"Java", []string{".java"}, javaFunctions},
}

// Language names the programming language of a file whose functions can
// be found, or returns "".
func Language(path string) string {
	if l := languageOf(path); l != nil {
		return l.name
	}
	return ""
}

func languageOf(path string) *language {
	ext := strings.ToLower(filepath.Ext(path))
	for i := range languages {
		if slices.Contains(languages[i].exts, ext) {
			return &languages[i]
		}
	}
	return nil
}

// Languages lists the languages whose functions can be found, such as
// "Go, Python, and Java".
func Languages() string {
	var names []string
	for _, l := range languages {
		names = append(names, l.name)
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// maxContext caps the context sent with each function: the file's imports
// and the line that starts its class or type.
const maxContext = 2048

// context is the code a function needs to be read: the file's imports and
// the line that starts its class or type.
func (c code) context(lines []string, f fn) string {
	var b strings.Builder
	for _, n := range c.imports {
		if b.Len()+len(lines[n-1]) > maxContext {
			b.WriteString("…\n")
			break
		}
		b.WriteString(lines[n-1])
		b.WriteByte('\n')
	}
	if f.parent > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(lines[f.parent-1], " \t"))
		b.WriteString("\n…")
	}
	return strings.TrimRight(b.String(), "\n")
}

// codeStructure breaks a source file between functions, and names each
// part by the function it is in. A function too large for one part is cut
// after blank lines.
func codeStructure(lines []string, c code) structure {
	s := structure{breaks: map[int]bool{}}
	for _, f := range c.fns {
		s.breaks[f.lead] = true
		s.breaks[f.end+1] = true
	}
	s.section = func(start, end int) string {
		var names []string
		for _, f := range c.fns {
			if f.lead <= end && start <= f.end {
				names = append(names, f.name)
			}
		}
		if len(names) > 3 {
			names = append(names[:3], "…")
		}
		return strings.Join(names, ", ")
	}
	inner := plainStructure(lines)
	inner.section = s.section
	s.within = &inner
	return s
}

// goFunctions finds the functions and methods of a Go file, and package
// variables set to a function.
func goFunctions(src string) (code, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return code{}, errLost
	}
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var c code
	for l := line(f.Package); l <= line(f.Name.End()); l++ {
		c.imports = append(c.imports, l)
	}
	unit := func(name string, decl ast.Node, doc *ast.CommentGroup) {
		u := fn{name: name, start: line(decl.Pos()), end: line(decl.End())}
		u.lead = u.start
		if doc != nil {
			u.lead = line(doc.Pos())
		}
		c.fns = append(c.fns, u)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue // implemented elsewhere, such as in assembly
			}
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = receiver(d.Recv.List[0].Type) + "." + name
			}
			unit(name, d, d.Doc)
		case *ast.GenDecl:
			switch d.Tok {
			case token.IMPORT:
				for l := line(d.Pos()); l <= line(d.End()); l++ {
					c.imports = append(c.imports, l)
				}
			case token.VAR:
				for _, s := range d.Specs {
					v := s.(*ast.ValueSpec)
					if hasFuncLit(v) {
						doc := v.Doc
						var node ast.Node = v
						if !d.Lparen.IsValid() {
							node, doc = d, d.Doc
						}
						unit(v.Names[0].Name, node, doc)
					}
				}
			}
		}
	}
	return c, nil
}

// receiver names a method's receiver type, without a pointer or type
// parameters.
func receiver(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return "?"
		}
	}
}

func hasFuncLit(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			found = true
		}
		return !found
	})
	return found
}
