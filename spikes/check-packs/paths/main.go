// Command paths writes one JSONL item per call path from an entry point
// (an HTTP route handler or an SSH or hook command) to a function that
// calls a sink, with the source of every function on the path.
//
// It was written for the check-packs spike and knows only Gogs: its entry
// points are any function in internal/route, cmd/gogs or internal/ssh,
// which stops some paths one call early.
//
//	paths -sink command|path|ssrf|sql ./gogs-snap > paths.jsonl
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

var sinks = map[string][]string{
	"command": {"os/exec.Command", "os/exec.CommandContext", "gogs.io/gogs/internal/process.", "github.com/gogs/git-module."},
	"path":    {"os.Open", "os.OpenFile", "os.Create", "os.Remove", "os.RemoveAll", "os.MkdirAll", "os.WriteFile", "os.ReadFile", "os.Rename", "os.Symlink", "os.Lstat", "os.Stat", "io/ioutil."},
	"ssrf":    {"(*net/http.Client).", "net/http.Get", "net/http.Post", "net/http.NewRequest", "gogs.io/gogs/internal/httplib.", "github.com/gogs/git-module.Clone", "github.com/gogs/git-module.IsURLAccessible", "(*github.com/gogs/git-module.Repository).Fetch"},
	"sql":     {"(*gorm.io/gorm.DB).Order", "(*gorm.io/gorm.DB).Raw", "(*gorm.io/gorm.DB).Where", "(*gorm.io/gorm.DB).Exec", "(*gorm.io/gorm.DB).Joins", "(*xorm.io/xorm.Session).", "(*xorm.io/xorm.Engine)."},
}

func isEntry(f *ssa.Function) bool {
	p := f.Pkg.Pkg.Path()
	return strings.HasPrefix(p, "gogs.io/gogs/internal/route") || p == "gogs.io/gogs/cmd/gogs" || p == "gogs.io/gogs/internal/ssh"
}

func own(f *ssa.Function) bool {
	return f != nil && f.Pkg != nil && strings.HasPrefix(f.Pkg.Pkg.Path(), "gogs.io/gogs") && f.Syntax() != nil &&
		!strings.HasSuffix(fset.Position(f.Pos()).Filename, "_test.go")
}

var (
	fset *token.FileSet
	root string
)

func main() {
	kind := flag.String("sink", "command", "")
	maxDepth := flag.Int("depth", 6, "")
	perSink := flag.Int("per", 3, "")
	flag.Parse()
	root, _ = filepath.Abs(flag.Arg(0))
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Dir: root}
	pkgs, err := packages.Load(cfg, "./internal/...", "./cmd/...")
	if err != nil {
		panic(err)
	}
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	fset = prog.Fset
	all := ssautil.AllFunctions(prog)
	cg := vta.CallGraph(all, cha.CallGraph(prog))
	cg.DeleteSyntheticNodes()

	// Functions of our own that call a sink directly.
	sinkCallers := map[*ssa.Function]string{}
	for f, n := range cg.Nodes {
		if !own(outer(f)) {
			continue
		}
		for _, e := range n.Out {
			name := e.Callee.Func.String()
			for _, s := range sinks[*kind] {
				if strings.HasPrefix(name, s) {
					sinkCallers[outer(f)] = name
				}
			}
		}
	}

	// Walk callers back from each sink caller, breadth first, to entry points.
	type item struct {
		Text  string   `json:"text"`
		Path  []string `json:"path"`
		Sink  string   `json:"sink"`
		Entry string   `json:"entry"`
	}
	enc := json.NewEncoder(os.Stdout)
	var keys []*ssa.Function
	for f := range sinkCallers {
		keys = append(keys, f)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, s := range keys {
		paths := back(cg, s, *maxDepth, *perSink)
		if len(paths) == 0 && isEntry(s) {
			paths = [][]*ssa.Function{{s}}
		}
		for _, p := range paths {
			var b strings.Builder
			var names []string
			for _, f := range p {
				names = append(names, name(f))
			}
			fmt.Fprintf(&b, "// Call path: %s, which calls %s.\n// The first function receives the outside request. Each function calls the next.\n\n", strings.Join(names, " -> "), sinkCallers[s])
			for _, f := range p {
				fmt.Fprintf(&b, "// %s\n%s\n\n", rel(f), source(f))
			}
			enc.Encode(item{Text: b.String(), Path: names, Sink: sinkCallers[s], Entry: names[0]})
		}
	}
}

func outer(f *ssa.Function) *ssa.Function {
	for f != nil && f.Parent() != nil {
		f = f.Parent()
	}
	return f
}

func name(f *ssa.Function) string {
	if r := f.Signature.Recv(); r != nil {
		t := r.Type().String()
		t = t[strings.LastIndex(t, ".")+1:]
		return t + "." + f.Name()
	}
	return f.Name()
}

func rel(f *ssa.Function) string {
	p := fset.Position(f.Pos())
	name, err := filepath.Rel(root, p.Filename)
	if err != nil {
		name = p.Filename
	}
	return fmt.Sprintf("%s#L%d", name, p.Line)
}

var files = map[string][]byte{}

func source(f *ssa.Function) string {
	syn := f.Syntax()
	if d, ok := syn.(*ast.FuncDecl); ok {
		syn = d
	}
	start, end := fset.Position(syn.Pos()), fset.Position(syn.End())
	b, ok := files[start.Filename]
	if !ok {
		b, _ = os.ReadFile(start.Filename)
		files[start.Filename] = b
	}
	return string(b[start.Offset:end.Offset])
}

// back finds up to n shortest caller chains from an entry point down to f.
func back(cg *callgraph.Graph, f *ssa.Function, depth, n int) [][]*ssa.Function {
	type st struct {
		f    *ssa.Function
		path []*ssa.Function
	}
	seen := map[*ssa.Function]bool{f: true}
	q := []st{{f, []*ssa.Function{f}}}
	var out [][]*ssa.Function
	for len(q) > 0 && len(out) < n {
		c := q[0]
		q = q[1:]
		if isEntry(c.f) && len(c.path) > 1 || (isEntry(c.f) && c.f == f) {
			out = append(out, c.path)
			continue
		}
		if len(c.path) >= depth {
			continue
		}
		node := cg.Nodes[c.f]
		if node == nil {
			continue
		}
		for _, e := range node.In {
			p := outer(e.Caller.Func)
			if !own(p) || seen[p] {
				continue
			}
			seen[p] = true
			q = append(q, st{p, append([]*ssa.Function{p}, c.path...)})
		}
	}
	return out
}
