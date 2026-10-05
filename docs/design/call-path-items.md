# Call-path items

Status: proposal. Written 2026-10-04.

> One item per path from where untrusted input enters to where it does
> harm, with the source of every function on the path, so decide can judge
> the flow and not only one function of it.

## Context

The [check-packs spike](check-packs.md#spike-results) found that most
false positives come from the unit, not the question. A function can't show
what its callers pass:

- Gogs' `GetByCollaboratorID` passes `orderBy` to GORM's `Order()`, so it
  scored 0.90 for SQL injection (CWE-89). Every caller passes a constant.
  As a call path from a route handler, it scored 0.33.
- OWASP Benchmark test cases that read a value through a helper class
  scored higher with the helper's code in the item: best scores for SQL
  injection went from 0.68 to 0.84, and for command injection from 0.66 to
  0.78.
- Paths that start from sinks are also fewer than functions: 2,030 Gogs
  functions became 17 SSRF paths and 151 command paths.

The spike's path builder, [`spikes/check-packs/paths`](../../spikes/check-packs/paths/main.go),
knows only Gogs. Its entry points are every function in three packages, so
it stopped `searchUserByName`'s path one call too early, and it names
Gogs' own wrappers as sinks. This proposal makes it general.

The [SDLC brainstorm](sdlc-brainstorm.md) cut exact analysis inside decide:
"the project's toolchain already does it." The spike reopens it for one
job only: building items. decide still judges, and still doesn't prove.

## Goals

- A Go project gets call-path items with no project setup.
- The flow templates of the universal pack (`sql-injection`,
  `command-injection`, `path-traversal`, `ssrf`, `code-injection` and
  `unsafe-deserialization`) work on path items with no change in wording.
- The number of items is bounded and known before a run, and each item
  fits in one request.
- The path builder is deterministic: the same code gives the same paths,
  so the answer cache holds.

## Non-goals

- Proof. A path is evidence that untrusted input can reach a sink. It is
  not a taint analysis, and it doesn't track values. The model reads the
  code and judges.
- Languages other than Go in the first release. TypeScript comes next;
  see [Languages](#languages).
- Replacing function items. Some bugs have no path to a sink (see
  [Failure modes](#failure-modes)).
- Bugs that span threads or requests, such as races and goroutine leaks.
  These need the callers too, but not a path from an entry point.

## Proposal

### Where the builder lives

A separate program, `decide-paths`, writes call paths as JSONL. decide
reads them as records, as it reads any JSONL today:

```sh
decide-paths --sink command ./... > paths.jsonl
decide run command-injection paths.jsonl --field text
```

Later, `--each path` runs `decide-paths` for the person (see
[Rollout](#rollout)).

- It is a command in this module, `cmd/decide-paths`, with its code in
  `internal/paths`. It is released beside `decide`, from the same tag.
- `decide` doesn't import `internal/paths`, so the `decide` binary doesn't
  link `golang.org/x/tools`, and runs on projects with no Go toolchain.
- `decide-paths` needs the Go toolchain and the project's modules, as
  `go vet` does. It loads packages with `golang.org/x/tools/go/packages`,
  builds SSA, and builds the call graph with VTA over CHA, as the spike
  did.

### Languages

Go first. Our own code is Go, the spike's labels are Go, and
`golang.org/x/tools` gives types, SSA and a call graph from one module.

TypeScript next, as a second program that writes the same JSONL. It needs
the TypeScript compiler and the project's `tsconfig.json`, so it can't live
in this Go module. Its design waits until the Go builder is measured.
Python and Java are not planned.

### Entry points

An entry point is a function where untrusted input enters. The spike's
templates call input "untrusted" when a remote user or another system
controls it, and they call flags, environment variables and configuration
trusted. Entry points follow the same rule.

A function is an entry point when one of its parameters has a request
type, or a struct type that embeds one:

| Kind | Types |
| --- | --- |
| HTTP, standard library | `*net/http.Request` |
| HTTP frameworks | `*gin.Context`, `echo.Context`, `*fiber.Ctx`, `*flamego.Context`, `*macaron.Context` |
| Connect | `*connect.Request[T]` |
| gRPC | a method of a type that embeds an `Unimplemented…Server` |

The embedding rule catches a framework context that a project wraps. Gogs'
`*context.Context` embeds `*macaron.Context`.

- **Outermost wins.** A helper that takes the request is an entry point
  too, by this rule. When the builder walks back from a sink and finds an
  entry point, it keeps walking while the caller is also an entry point,
  and starts the path at the last one. In the spike, `searchUserByName`'s
  path started at `RenderUserSearch`, a helper that takes the request;
  its caller `ExploreUsers` is where the request's values come from.
- **`main` is not an entry point.** Its input is flags and environment.
- **A project can add entry points** with `--entry`, a pattern of package
  and function, such as `--entry 'gogs.io/gogs/cmd/gogs.runServ'`. Gogs'
  `serv` command reads `SSH_ORIGINAL_COMMAND`, which a remote user sets.
  No rule finds that.
- **It prints what it found**, to stderr: "Found 214 entry points: 198 HTTP
  handlers, 16 gRPC methods." A person can see a missing kind of entry
  before a run.

### Sinks

A sink is a call that does harm with untrusted input. `decide-paths` has
one built-in list per flow template:

| `--sink` | Template | Examples |
| --- | --- | --- |
| `sql` | `sql-injection` | `(*database/sql.DB).Query`, `Exec`; GORM's `Raw`, `Where`, `Order`; sqlx and pgx |
| `command` | `command-injection` | `os/exec.Command`, `CommandContext`, `syscall.Exec` |
| `path` | `path-traversal` | `os.Open`, `OpenFile`, `Create`, `Remove`, `RemoveAll`, `WriteFile`, `ReadFile`, `Rename`, `Symlink` |
| `ssrf` | `ssrf` | `(*net/http.Client).Do`, `Get`, `Post`; `net/http.NewRequest`, `net.Dial` |
| `code` | `code-injection` | `text/template.Parse`, `plugin.Open`, embedded interpreters |
| `deserialize` | `unsafe-deserialization` | `encoding/gob`, `yaml.Unmarshal` into `interface{}` |

Two rules keep the lists short and the paths few:

- **Sinks reached through dependencies count.** When a project calls a
  dependency, and that dependency calls a sink by static calls, the
  project's call is a sink call. Gogs calls `git-module.Clone`, which runs
  `git`. The spike had to name `git-module` by hand. Only static calls
  count inside dependencies. Interface calls would reach every sink.
- **Constant arguments drop.** A sink call whose input argument is a
  constant can't carry untrusted input: `exec.Command("git", "version")`,
  or `db.Where("id = ?", id)`, where the SQL is constant and the value is a
  parameter. Each sink names its input argument. This rule is local to the
  call. A constant passed down from a caller stays a path, and the model
  judges it, as it did for `orderBy`.

`--sink-call` adds a project's own sink, by pattern. The
[`where` proposal](check-packs.md#sequence-one-pr-each) may move the sink
lists into each `template.json`. Until then they live in `decide-paths`.

### Building paths

The builder walks back from sinks, not forward from entry points. Forward
walks from every handler reach most of the program.

1. Find each call site in the project's code that calls a sink, after the
   rules above.
2. From the function that holds it, walk back through callers,
   breadth first, to an entry point, with the outermost rule.
3. Keep up to 3 paths per call site, shortest first, each from a
   different entry point when there is one. `--per` changes the number.
4. Stop at 8 functions deep. `--depth` changes it.

A closure belongs to the function that holds it, as in the spike. Test
files are skipped.

### The item

One JSONL record per path:

```json
{
  "text": "// Call path: ...",
  "source": "internal/database/repositories.go#L181",
  "entry": "user.Dashboard",
  "path": ["user.Dashboard", "RepositoriesStore.GetByCollaboratorID"],
  "sink": "(*gorm.io/gorm.DB).Order",
  "kind": "sql"
}
```

`source` is the sink call's file and line. `text` is what the model
reads: a header, then each function's source with its file and line, from
the entry point down:

```go
// Call path: user.Dashboard -> RepositoriesStore.GetByCollaboratorID,
// which calls (*gorm.io/gorm.DB).Order at internal/database/repositories.go#L181.
// The first function receives the request. Each function calls the next.

// internal/route/user/home.go#L102
func Dashboard(c *context.Context) { ... }

// internal/database/repositories.go#L167
func (s *RepositoriesStore) GetByCollaboratorID(ctx context.Context, ...) { ... }
```

**Size.** Each record's `text` fits under decide's item limit (64 KB, less
the room for questions), so decide never cuts a path into parts. A cut
path loses the flow. When a path is too large, the builder shortens the
middle functions, never the first or the last: it keeps each one's
signature and the lines around the call to the next, and writes `// 140
lines left out`. The largest Gogs path in the spike was 28 KB.

**Labels.** Today decide labels a record by its file and line, such as
`paths.jsonl:3`, so `--format github` would annotate the JSONL file. The
`--json` output carries the whole record, so the plugin and scripts have
`source`. `--each path` labels each item by `source` (see
[Rollout](#rollout)).

### Bounds

| Bound | Default | Why |
| --- | --- | --- |
| Paths per sink call | 3 | The spike's value. In Gogs, 123 of 291 SQL sink functions reached the cap. See [Open questions](#open-questions). |
| Depth | 8 functions | The spike used 6, and its longest paths reached it, so some were cut. |
| Item size | under decide's limit | A cut path loses the flow. |
| Items in a run | none | `decide run --dry-run` counts them, and `--limit` caps them, as for any run. |

`decide-paths` prints its counts to stderr, such as "151 paths to 86 sink
calls. 12 sink calls have no path from an entry point." In the spike,
Gogs had 17 SSRF paths, 124 file paths, 151 command paths and 607 SQL
paths, and the largest was 28 KB.

### How the plugin uses it

`/decide:hunt` and the universal-pack skill run `decide-paths` when the
project is Go and the template is a flow template, then `decide run` on
the paths. Claude confirms the top results by reading the path and its
callers, as it does for function items. A sink call with no path is
reported as a count, and its function is still judged by function.

## Alternatives considered

- **`--each path` inside `decide`, from the start.** One command for the
  person. It links `golang.org/x/tools` into a binary that runs on any
  project, and puts one language's analysis inside the generic CLI before
  we know it works. We get there later by running `decide-paths`, as git
  runs `git-foo`.
- **Claude builds the paths with an LSP or grep.** It works in any
  language, but it isn't deterministic, so the cache misses and two runs
  disagree. It costs model calls per path. It suits confirming, not
  sweeping.
- **CodeQL or Semgrep paths.** CodeQL finds taint paths well, but its
  license restricts use on closed-source code, and it is heavy to set up.
  Semgrep's free taint mode stays inside one function. Either could feed
  decide the same JSONL; we don't depend on them.
- **Callers as context (`--context calls`).** A function item plus its
  direct callers. It fixes the `orderBy` case, which needed one hop, and
  it works in any language decide parses today. It can't show where input
  enters, so trust stays a guess. It is a candidate for function items
  that have no path, not a replacement.
- **Forward from entry points.** Every handler reaches most of the
  program through shared helpers. Backward from sinks gives fewer paths,
  and every one ends at a sink.

## Failure modes

- **No path to the sink.** In the spike, `UserPath` builds a path but
  opens no file, and `isRepositoryGitPath` is a check, not a sink. Both
  were Gogs path-traversal bugs, and neither has a path. Function items
  still find them, so path mode adds to function mode for now.
- **A missing entry point.** A queue consumer, a webhook from another
  service, or a framework not in the table. Its paths are lost with no
  error. The counts on stderr and `--entry` are the answer. Each new
  framework is one row in the table.
- **Call graph errors.** VTA over-approximates interface calls, so some
  paths can't happen at runtime. The model judges them, and they cost
  requests. Calls through reflection are missed.
- **Packages that don't type-check**, such as code behind build tags or
  cgo. `decide-paths` skips them and names them, as decide names files it
  can't parse.
- **Large projects.** SSA and VTA for a large module take time and memory.
  We measure this on Gogs and one larger Go project before release.

## Security considerations

- `decide-paths` runs `go list` on the project, as `go vet` does. That can
  download modules, and, with a `toolchain` line in `go.mod`, a Go
  toolchain. Run it on code you would build.
- Path text is project code, so it is untrusted when decide prints it.
  decide already cleans record values before it prints them.
- It reads only the project and its module cache, and writes only to
  stdout and stderr.

## Rollout

1. **`decide-paths` for Go**, with the entry and sink rules above, tests on
   a fixture module in `testdata/`, and a planted path in `demo/`. Measure
   it against the spike on Gogs: each label's rank, the items flagged at
   0.5, and the requests per sweep. Add a second Go project with labeled
   fixes, and one of ours to count false positives. It ships when path
   mode ranks the labels that have a path at least as high as function
   mode, with fewer items flagged.
2. **The plugin** runs it for flow templates on Go projects.
3. **`--each path`.** decide runs `decide-paths` from `PATH`, passes the
   template's sinks, labels each item by its sink call, and names the unit
   `path`. This needs the [`where` proposal](check-packs.md#sequence-one-pr-each),
   so that templates name their sinks.
4. **TypeScript**, as a second builder that writes the same JSONL.

## Open questions

1. Should flow templates on Go run both path and function items by default,
   or path items, with function items only for sink calls that have no
   path? Both cost about twice the requests. The answer needs the
   measurements in step 1.
2. Should `decide-paths` take a template name (`--template ssrf`) instead
   of a sink kind, so the template and its sinks can't drift apart?
3. Is 3 paths per sink call enough when a sink is shared, such as one
   `exec` helper that every command goes through? Then the sink call is in
   the helper, and 3 paths show 3 of many callers. Treating a function
   that only forwards its arguments to a sink as a sink may fix this.
