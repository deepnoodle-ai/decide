// Command builder writes one JSONL item per call path from an entry point
// to a sink call, by the rules in docs/design/call-path-items.md. It is the
// spike for that proposal, not decide-paths: it has no tests, and its
// sink lists cover only what the Gogs measurement needs.
//
//	builder -sink command|path|ssrf|sql [-orphans] [-entry pattern] ./project > paths.jsonl
//
// Rules, as proposed:
//   - Entry points: a function, closures included, with a parameter of a
//     request type, or of a struct that embeds one. The path walks up over
//     static calls while the caller is an entry point too.
//   - Sinks: a built-in list, with input arguments. A project function that
//     passes a parameter to a sink's input is a sink. A call into a
//     dependency that reaches a sink by static calls is a sink call. A call
//     whose input arguments are all constant is dropped.
//   - Paths: back from each sink call, breadth first, up to -per paths from
//     different entry points, at most -depth functions.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
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

// A sink spec maps a function's full SSA name to the indexes of its input
// arguments, not counting the receiver.
var sinkSpecs = map[string]map[string][]int{
	"command": {
		"os/exec.Command": {0, 1}, "os/exec.CommandContext": {1, 2}, "syscall.Exec": {0, 1},
	},
	"path": {
		"os.Open": {0}, "os.OpenFile": {0}, "os.Create": {0}, "os.Remove": {0}, "os.RemoveAll": {0},
		"os.WriteFile": {0}, "os.ReadFile": {0}, "os.Rename": {0, 1}, "os.Symlink": {0, 1},
		"os.Mkdir": {0}, "os.MkdirAll": {0}, "io/ioutil.ReadFile": {0}, "io/ioutil.WriteFile": {0},
	},
	"ssrf": {
		"(*net/http.Client).Do": {0}, "(*net/http.Client).Get": {0}, "(*net/http.Client).Post": {0},
		"(*net/http.Client).Head": {0}, "(*net/http.Client).PostForm": {0},
		"net/http.Get": {0}, "net/http.Post": {0}, "net/http.Head": {0}, "net/http.PostForm": {0},
		"net.Dial": {1}, "net.DialTimeout": {1},
	},
	"sql": sqlSinks(),
}

func sqlSinks() map[string][]int {
	m := map[string][]int{}
	for _, f := range []string{"Query", "Exec", "QueryRow"} {
		m["(*database/sql.DB)."+f] = []int{0}
		m["(*database/sql.DB)."+f+"Context"] = []int{1}
		m["(*database/sql.Tx)."+f] = []int{0}
		m["(*database/sql.Tx)."+f+"Context"] = []int{1}
	}
	for _, f := range []string{"Raw", "Where", "Order", "Exec", "Joins", "Select", "Group", "Having", "Not", "Or"} {
		m["(*gorm.io/gorm.DB)."+f] = []int{0}
	}
	for _, t := range []string{"(*xorm.io/xorm.Session).", "(*xorm.io/xorm.Engine)."} {
		for _, f := range []string{"Where", "And", "Or", "OrderBy", "SQL", "Sql", "Exec", "Query", "QueryString", "Select", "GroupBy", "Having"} {
			m[t+f] = []int{0}
		}
		m[t+"Join"] = []int{1, 2}
	}
	return m
}

// Request types, by package path and type name.
var requestTypes = map[string]bool{
	"net/http.Request":                       true,
	"github.com/gin-gonic/gin.Context":       true,
	"github.com/labstack/echo/v4.Context":    true,
	"github.com/gofiber/fiber/v2.Ctx":        true,
	"github.com/flamego/flamego.Context":     true,
	"gopkg.in/macaron.v1.Context":            true,
	"connectrpc.com/connect.Request":         true,
	"github.com/bufbuild/connect-go.Request": true,
}

var (
	carry   bool
	fset    *token.FileSet
	root    string
	modPath string
	entries []string
)

type sinkFn struct {
	inputs []int  // SSA parameter indexes, receiver included
	kind   string // "direct", "forward" or "dependency"
	via    string // the chain down to the listed sink
}

type site struct {
	fn     *ssa.Function
	pos    token.Pos
	callee string
	via    string
	kind   string
}

func main() {
	kind := flag.String("sink", "command", "command, path, ssrf or sql")
	depth := flag.Int("depth", 8, "most functions on a path")
	per := flag.Int("per", 3, "most paths per sink call")
	orphans := flag.Bool("orphans", false, "also write the function of each sink call with no path")
	noDeps := flag.Bool("no-deps", false, "skip the dependency rule")
	flag.BoolVar(&carry, "carry", false, "stop at functions that take no input, and at go statements")
	entry := flag.String("entry", "", "comma-separated function name prefixes to treat as entry points")
	flag.Parse()
	if *entry != "" {
		entries = strings.Split(*entry, ",")
	}
	root, _ = filepath.Abs(flag.Arg(0))
	cfg := &packages.Config{Mode: packages.LoadAllSyntax | packages.NeedModule, Dir: root, Tests: false}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		die(err)
	}
	var broken []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if len(p.Errors) > 0 && p.Module != nil && p.Module.Main {
			broken = append(broken, p.PkgPath)
		}
	})
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main {
			modPath = p.Module.Path
			break
		}
	}
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	fset = prog.Fset
	all := ssautil.AllFunctions(prog)
	cg := vta.CallGraph(all, cha.CallGraph(prog))
	cg.DeleteSyntheticNodes()

	// Listed sinks.
	sinks := map[*ssa.Function]sinkFn{}
	for f := range all {
		if in, ok := sinkSpecs[*kind][fullName(f)]; ok {
			off := 0
			if f.Signature.Recv() != nil {
				off = 1
			}
			var idx []int
			for _, i := range in {
				idx = append(idx, i+off)
			}
			sinks[f] = sinkFn{inputs: idx, kind: "direct", via: fullName(f)}
		}
	}
	listed := len(sinks)

	// Dependencies that reach a listed sink by static calls.
	if !*noDeps {
		callers := map[*ssa.Function][]*ssa.Function{}
		for f := range all {
			if own(f) || f.Blocks == nil {
				continue
			}
			for _, b := range f.Blocks {
				for _, in := range b.Instrs {
					if c, ok := in.(ssa.CallInstruction); ok {
						if g := c.Common().StaticCallee(); g != nil {
							callers[origin(g)] = append(callers[origin(g)], f)
						}
					}
				}
			}
		}
		// Sorted, so each dependency's chain down to a sink is the same on
		// every run.
		var q []*ssa.Function
		for f := range sinks {
			q = append(q, f)
		}
		sort.Slice(q, func(i, j int) bool { return fullName(q[i]) < fullName(q[j]) })
		for _, cs := range callers {
			sort.Slice(cs, func(i, j int) bool { return fullName(cs[i]) < fullName(cs[j]) })
		}
		for len(q) > 0 {
			f := q[0]
			q = q[1:]
			for _, c := range callers[f] {
				c = origin(c)
				if _, ok := sinks[c]; ok || own(c) {
					continue
				}
				sinks[c] = sinkFn{kind: "dependency", via: fullName(c) + " -> " + sinks[f].via}
				q = append(q, c)
			}
		}
	}
	deps := len(sinks) - listed

	// Forwarders, to a fixed point: a project function that passes one of
	// its parameters to a sink's input is a sink.
	forwarders := 0
	for changed := true; changed; {
		changed = false
		for _, f := range sortedOwn(all) {
			if _, ok := sinks[f]; ok {
				continue
			}
			if idx := forwarded(f, sinks); len(idx) > 0 {
				sinks[f] = sinkFn{inputs: idx, kind: "forward", via: fullName(f) + " -> " + viaOf(f, sinks)}
				forwarders++
				changed = true
			}
		}
	}
	var sites []site
	for _, f := range sortedOwn(all) {
		sites = append(sites, sitesOf(f, sinks)...)
	}

	entryCount := 0
	for _, f := range sortedOwn(all) {
		if isEntry(f) {
			entryCount++
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	nPaths, noPath, cut, trimmed := 0, 0, 0, 0
	for _, s := range sites {
		paths := back(cg, s.fn, *depth, *per)
		if len(paths) == 0 {
			noPath++
			if *orphans {
				text, ok := orphanText(outer(s.fn), s)
				if !ok {
					cut++
					continue
				}
				enc.Encode(record{Text: text, Source: rel(s.pos), Path: []string{name(s.fn)}, Sink: s.callee, Via: s.via, Rule: s.kind, Orphan: true})
			}
			continue
		}
		for _, p := range paths {
			text, t, ok := render(p, s)
			if t {
				trimmed++
			}
			if !ok {
				cut++
				continue
			}
			var names []string
			for _, st := range p {
				names = append(names, name(st.fn))
			}
			enc.Encode(record{Text: text, Source: rel(s.pos), Entry: names[0], Path: names, Sink: s.callee, Via: s.via, Rule: s.kind})
			nPaths++
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %d entry points; sinks: %d listed, %d dependency, %d forwarders; %d sink calls; %d paths; %d sink calls with no path; %d items shortened, %d skipped as too large\n",
		*kind, entryCount, listed, deps, forwarders, len(sites), nPaths, noPath, trimmed, cut)
	if len(broken) > 0 {
		fmt.Fprintf(os.Stderr, "packages with errors: %s\n", strings.Join(broken, ", "))
	}
}

type record struct {
	Text   string   `json:"text"`
	Source string   `json:"source"`
	Entry  string   `json:"entry,omitempty"`
	Path   []string `json:"path"`
	Sink   string   `json:"sink"`
	Via    string   `json:"via"`
	Rule   string   `json:"rule"`
	Orphan bool     `json:"orphan,omitempty"`
}

// sitesOf lists f's sink calls, now that the forwarders are known. A call
// whose non-constant inputs all come from f's parameters is not a site:
// f is a forwarder, and its callers hold the sites.
func sitesOf(f *ssa.Function, sinks map[*ssa.Function]sinkFn) []site {
	var out []site
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			c, ok := in.(ssa.CallInstruction)
			if !ok {
				continue
			}
			g := c.Common().StaticCallee()
			if g == nil {
				continue
			}
			s, ok := sinks[origin(g)]
			if !ok || origin(g) == f {
				continue
			}
			if s.kind == "dependency" {
				out = append(out, site{f, c.Pos(), fullName(origin(g)), s.via, s.kind})
				continue
			}
			args := c.Common().Args
			local := false
			for _, i := range s.inputs {
				if i < len(args) && !isConst(args[i]) && len(params(f, args[i])) == 0 {
					local = true
				}
			}
			if local {
				out = append(out, site{f, c.Pos(), fullName(origin(g)), s.via, s.kind})
			}
		}
	}
	return out
}

// forwarded lists the parameters of f that reach a sink's input argument.
func forwarded(f *ssa.Function, sinks map[*ssa.Function]sinkFn) []int {
	fwd := map[int]bool{}
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			c, ok := in.(ssa.CallInstruction)
			if !ok {
				continue
			}
			g := c.Common().StaticCallee()
			if g == nil {
				continue
			}
			s, ok := sinks[origin(g)]
			if !ok || s.kind == "dependency" || origin(g) == f {
				continue
			}
			args := c.Common().Args
			for _, i := range s.inputs {
				if i < len(args) && !isConst(args[i]) {
					for _, p := range params(f, args[i]) {
						fwd[p] = true
					}
				}
			}
		}
	}
	var idx []int
	for i := range fwd {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	return idx
}

func viaOf(f *ssa.Function, sinks map[*ssa.Function]sinkFn) string {
	for _, b := range f.Blocks {
		for _, in := range b.Instrs {
			if c, ok := in.(ssa.CallInstruction); ok {
				if g := c.Common().StaticCallee(); g != nil && origin(g) != f {
					if s, ok := sinks[origin(g)]; ok {
						return s.via
					}
				}
			}
		}
	}
	return "?"
}

// isConst reports whether v is a constant, through conversions, and for a
// variadic slice, whether every value stored in it is.
func isConst(v ssa.Value) bool {
	v = unwrap(v)
	switch v := v.(type) {
	case *ssa.Const:
		return true
	case *ssa.Slice:
		a, ok := v.X.(*ssa.Alloc)
		if !ok {
			return false
		}
		for _, st := range stores(a) {
			if !isConst(st) {
				return false
			}
		}
		return true
	}
	return false
}

// params returns the indexes of f's parameters that v comes from directly.
func params(f *ssa.Function, v ssa.Value) []int {
	v = unwrap(v)
	var out []int
	switch v := v.(type) {
	case *ssa.Parameter:
		for i, p := range f.Params {
			if p == v {
				out = append(out, i)
			}
		}
	case *ssa.Slice:
		if a, ok := v.X.(*ssa.Alloc); ok {
			for _, st := range stores(a) {
				out = append(out, params(f, st)...)
			}
		} else {
			out = append(out, params(f, v.X)...)
		}
	}
	return out
}

func unwrap(v ssa.Value) ssa.Value {
	for {
		switch w := v.(type) {
		case *ssa.MakeInterface:
			v = w.X
		case *ssa.ChangeType:
			v = w.X
		case *ssa.Convert:
			v = w.X
		case *ssa.ChangeInterface:
			v = w.X
		default:
			return v
		}
	}
}

// stores lists the values stored into the elements of a slice's array.
func stores(a *ssa.Alloc) []ssa.Value {
	var out []ssa.Value
	for _, r := range *a.Referrers() {
		ia, ok := r.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		for _, r2 := range *ia.Referrers() {
			if st, ok := r2.(*ssa.Store); ok && st.Addr == ia {
				out = append(out, st.Val)
			}
		}
	}
	return out
}

func isRequest(t types.Type, depth int) bool {
	if depth > 3 {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	if o := n.Origin().Obj(); o.Pkg() != nil && requestTypes[o.Pkg().Path()+"."+o.Name()] {
		return true
	}
	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if fl := st.Field(i); fl.Embedded() && isRequest(fl.Type(), depth+1) {
			return true
		}
	}
	return false
}

func isEntry(f *ssa.Function) bool {
	if f.Name() == "main" && f.Parent() == nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(fullName(f), e) {
			return true
		}
	}
	ps := f.Signature.Params()
	for i := 0; i < ps.Len(); i++ {
		if isRequest(ps.At(i).Type(), 0) {
			return true
		}
	}
	// A gRPC method: its receiver embeds an Unimplemented...Server.
	if r := f.Signature.Recv(); r != nil {
		t := r.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if st, ok := t.Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				n := st.Field(i).Name()
				if st.Field(i).Embedded() && strings.HasPrefix(n, "Unimplemented") && strings.HasSuffix(n, "Server") {
					return true
				}
			}
		}
	}
	return false
}

func own(f *ssa.Function) bool {
	if f == nil || f.Pkg == nil || f.Syntax() == nil {
		return false
	}
	p := f.Pkg.Pkg.Path()
	if p != modPath && !strings.HasPrefix(p, modPath+"/") {
		return false
	}
	return !strings.HasSuffix(fset.Position(f.Pos()).Filename, "_test.go")
}

func origin(f *ssa.Function) *ssa.Function {
	if o := f.Origin(); o != nil {
		return o
	}
	return f
}

func fullName(f *ssa.Function) string { return origin(f).String() }

func sortedOwn(all map[*ssa.Function]bool) []*ssa.Function {
	var out []*ssa.Function
	for f := range all {
		if own(f) && f.Origin() == nil && f.Blocks != nil {
			out = append(out, f)
		}
	}
	// By file and offset, not token.Pos: packages load in parallel, so
	// positions differ between runs.
	sort.Slice(out, func(i, j int) bool {
		a, b := fset.Position(out[i].Pos()), fset.Position(out[j].Pos())
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Offset != b.Offset {
			return a.Offset < b.Offset
		}
		return out[i].String() < out[j].String()
	})
	return out
}

// step is one function on a path, and the call it makes to the next.
type step struct {
	fn   *ssa.Function
	call token.Pos
}

// back finds up to n paths from an entry point down to f, shortest first,
// each from a different entry point.
func back(cg *callgraph.Graph, f *ssa.Function, depth, n int) [][]step {
	type st struct{ path []step }
	seen := map[*ssa.Function]bool{f: true}
	q := []st{{[]step{{fn: f}}}}
	var out [][]step
	for len(q) > 0 && len(out) < n {
		c := q[0]
		q = q[1:]
		head := c.path[0].fn
		if isEntry(head) {
			out = append(out, outermost(cg, c.path, depth))
			continue
		}
		if len(c.path) >= depth || carry && noInput(head) {
			continue
		}
		for _, e := range callersOf(cg, head) {
			if seen[e.fn] {
				continue
			}
			seen[e.fn] = true
			q = append(q, st{append([]step{e}, c.path...)})
		}
	}
	return out
}

// callersOf lists f's callers in the project, sorted. A closure that only
// a dependency calls, such as a callback, gets the function that holds it.
func callersOf(cg *callgraph.Graph, f *ssa.Function) []step {
	var out []step
	if node := cg.Nodes[f]; node != nil {
		for _, e := range node.In {
			if _, isGo := e.Site.(*ssa.Go); carry && isGo {
				continue
			}
			if own(e.Caller.Func) {
				out = append(out, step{e.Caller.Func, e.Pos()})
			}
		}
	}
	if len(out) == 0 && f.Parent() != nil && own(f.Parent()) {
		out = append(out, step{f.Parent(), f.Pos()})
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].fn.String(), out[j].fn.String(); a != b {
			return a < b
		}
		return fset.Position(out[i].call).Offset < fset.Position(out[j].call).Offset
	})
	return out
}

// noInput reports whether f takes nothing from its caller: no parameters,
// no receiver, and no captured variables. Its callers can't pass it a
// request's values, so a path doesn't go up through it.
func noInput(f *ssa.Function) bool {
	return len(f.Params) == 0 && len(f.FreeVars) == 0
}

// outermost walks up from an entry point while a caller is an entry point
// too and calls it by a static call.
func outermost(cg *callgraph.Graph, path []step, depth int) []step {
	on := map[*ssa.Function]bool{}
	for _, s := range path {
		on[s.fn] = true
	}
	for len(path) < depth {
		node := cg.Nodes[path[0].fn]
		if node == nil {
			break
		}
		var next *step
		var ins []*callgraph.Edge
		ins = append(ins, node.In...)
		sort.Slice(ins, func(i, j int) bool { return ins[i].Caller.Func.String() < ins[j].Caller.Func.String() })
		for _, e := range ins {
			c := e.Caller.Func
			if !own(c) || on[c] || !isEntry(c) || e.Site == nil || e.Site.Common().StaticCallee() == nil {
				continue
			}
			next = &step{c, e.Pos()}
			break
		}
		if next == nil {
			break
		}
		on[next.fn] = true
		path = append([]step{*next}, path...)
	}
	return path
}

const budget = 64<<10 - 1024 - 200

func jsonSize(s string) int {
	b, _ := json.Marshal(s)
	return len(b)
}

// render writes a path's text. It returns whether it shortened any
// function, and false for ok when the path can't fit under budget.
func render(p []step, s site) (text string, trimmed, ok bool) {
	var names []string
	for _, st := range p {
		names = append(names, name(st.fn))
	}
	head := fmt.Sprintf("// Call path: %s,\n// which calls %s at %s.\n", strings.Join(names, " -> "), s.callee, rel(s.pos))
	if s.via != s.callee && s.via != "" {
		head += fmt.Sprintf("// That call leads to %s.\n", s.via)
	}
	head += "// The first function receives the request. Each function calls the next.\n\n"
	bodies := make([]string, len(p))
	for i, st := range p {
		bodies[i] = section(st.fn, -1)
	}
	text = head + strings.Join(bodies, "\n")
	if jsonSize(text) <= budget {
		return text, false, true
	}
	// Shorten the middle functions first, then the one that holds the sink
	// call, then the entry point. Each keeps the lines around its call.
	order := []int{}
	for i := 1; i < len(p)-1; i++ {
		order = append(order, i)
	}
	order = append(order, len(p)-1)
	if len(p) > 1 {
		order = append(order, 0)
	}
	for _, i := range order {
		keep := line(s.pos)
		if i < len(p)-1 {
			keep = line(p[i+1].call)
		}
		bodies[i] = section(p[i].fn, keep)
		text = head + strings.Join(bodies, "\n")
		if jsonSize(text) <= budget {
			return text, true, true
		}
	}
	// Still too large: skip it, so decide never cuts a path into parts.
	return "", true, false
}

// orphanText is the function that holds a sink call with no path, shortened
// to the lines around the call when it is too large, or false when even
// that is.
func orphanText(f *ssa.Function, s site) (string, bool) {
	text := section(f, -1)
	if jsonSize(text) <= budget {
		return text, true
	}
	text = section(f, line(s.pos))
	return text, jsonSize(text) <= budget
}

// section is a function's source with its location, or, when keep is a
// line number, its first line, the lines around keep, and its last line.
func section(f *ssa.Function, keep int) string {
	src := source(f)
	loc := rel(f.Pos())
	if f.Parent() != nil {
		loc += fmt.Sprintf(", a closure in %s", name(outer(f)))
	}
	if keep < 0 {
		return fmt.Sprintf("// %s\n%s\n", loc, src)
	}
	lines := strings.Split(src, "\n")
	first := fset.Position(f.Syntax().Pos()).Line
	k := keep - first
	var b strings.Builder
	b.WriteString(lines[0] + "\n")
	lo, hi := max(1, k-3), min(len(lines)-1, k+4)
	lo = min(lo, len(lines)-1)
	hi = max(hi, lo)
	if lo > 1 {
		fmt.Fprintf(&b, "\t// %d lines left out\n", lo-1)
	}
	for i := lo; i < hi; i++ {
		b.WriteString(lines[i] + "\n")
	}
	if hi < len(lines)-1 {
		fmt.Fprintf(&b, "\t// %d lines left out\n", len(lines)-1-hi)
	}
	b.WriteString(lines[len(lines)-1] + "\n")
	return fmt.Sprintf("// %s\n%s", loc, b.String())
}

func line(p token.Pos) int { return fset.Position(p).Line }

func outer(f *ssa.Function) *ssa.Function {
	for f.Parent() != nil {
		f = f.Parent()
	}
	return f
}

func name(f *ssa.Function) string {
	if f.Parent() != nil {
		return name(outer(f)) + "." + f.Name()
	}
	if r := f.Signature.Recv(); r != nil {
		t := r.Type().String()
		t = t[strings.LastIndex(t, ".")+1:]
		return t + "." + f.Name()
	}
	return f.Name()
}

func rel(pos token.Pos) string {
	p := fset.Position(pos)
	n, err := filepath.Rel(root, p.Filename)
	if err != nil {
		n = p.Filename
	}
	return fmt.Sprintf("%s#L%d", n, p.Line)
}

var files = map[string][]byte{}

func source(f *ssa.Function) string {
	var syn ast.Node = f.Syntax()
	start, end := fset.Position(syn.Pos()), fset.Position(syn.End())
	b, ok := files[start.Filename]
	if !ok {
		b, _ = os.ReadFile(start.Filename)
		files[start.Filename] = b
	}
	return string(b[start.Offset:end.Offset])
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
