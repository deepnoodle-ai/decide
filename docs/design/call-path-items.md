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
Gogs' own wrappers and `git-module` as sinks by hand. This proposal makes
it general.

The [SDLC brainstorm](sdlc-brainstorm.md) cut exact analysis inside decide:
"the project's toolchain already does it." The spike reopens it for one
job only: building items. decide still judges, and still doesn't prove.

## Goals

- A Go project gets call-path items with no project setup.
- The four flow templates the spike measured, `sql-injection` (CWE-89),
  `command-injection` (CWE-78), `path-traversal` (CWE-22) and `ssrf`
  (CWE-918), work on path items with no change in wording.
- The number of items is bounded and known before a run, and decide never
  cuts a path into parts.
- The path builder is deterministic: the same code gives the same paths,
  so the answer cache holds.

## Non-goals

- Proof. A path is evidence that untrusted input can reach a sink. It is
  not a taint analysis, and it doesn't track values. The model reads the
  code and judges.
- Languages other than Go in the first release. TypeScript comes next;
  see [Languages](#languages).
- Replacing function items. Some flows have no path from a request (see
  [What has no path](#what-has-no-path)).
- Other flow templates, such as `code-injection`. Each gets its sinks when
  its template is built and measured.
- Bugs that span threads or requests, such as races and goroutine leaks.
  These need the callers too, but not a path from an entry point.

## Proposal

### Where the builder lives

A separate program, `decide-paths`, writes call paths as JSONL. decide
reads them as records:

```sh
decide-paths --sink command ./... > paths.jsonl
decide run command-injection paths.jsonl --field text
```

Later, `--each path` runs `decide-paths` for the person (see
[Rollout](#rollout)).

- It is a command in this module, `cmd/decide-paths`, with its code in
  `internal/paths`. `decide` doesn't import `internal/paths`, so the
  `decide` binary doesn't link `golang.org/x/tools`, and still runs on
  projects with no Go toolchain.
- It is installed with `go install
  github.com/deepnoodle-ai/decide/cmd/decide-paths@<version>`, not as a
  release binary. `go/packages` type-checks with the `go/types` built into
  the program, so a binary built with Go 1.N fails on a module that uses
  Go 1.N+1. `go install` builds it with the person's own toolchain, which
  is the one their project builds with. The plugin checks for it and
  prints the install command when it is missing.
- It loads packages with `golang.org/x/tools/go/packages`, builds SSA, and
  builds the call graph with VTA over CHA, as the spike did.

**decide change.** The flow templates default to `"each": "function"`.
Today a JSONL file named with that default fails: "is not in Go…; use
--each file". The spike ran copies of the templates without `each`. In
step 1, a dataset (JSONL, JSON or CSV) is read as records when the unit
comes from the template's default. An explicit `--each function` still
fails on it. A test covers both.

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

A function is an entry point when its own signature has a parameter of a
request type, or of a struct type that embeds one:

| Kind | Types |
| --- | --- |
| HTTP, standard library | `*net/http.Request` |
| HTTP frameworks | `*gin.Context`, `echo.Context`, `*fiber.Ctx`, `*flamego.Context`, `*macaron.Context` |
| Connect | `*connect.Request[T]` |
| gRPC | a method of a type that embeds an `Unimplemented…Server` |

- **Closures count.** The rule tests each function's own signature,
  closures included. `http.HandleFunc("/x", func(w, r) {…})` inside `main`
  is an entry point, and so is the `http.HandlerFunc` that a method such
  as `func (s *server) handleX() http.HandlerFunc` returns. The path starts
  at the closure, and its header names the function that holds it. Only
  the closure's source is in the item, not all of `main`.
- **Embedding.** The embedding rule catches a framework context that a
  project wraps. Gogs' `*context.Context` embeds `*macaron.Context`.
- **Outermost wins, over static calls.** A helper that takes the request
  is an entry point too, by this rule. When the builder walks back and
  finds an entry point, it keeps walking while a caller is also an entry
  point and calls it by a static call, and starts the path at the last
  one. In Gogs, `searchUserByName`'s path started at `RenderUserSearch`, a
  helper that takes the request; `ExploreUsers` calls it statically and is
  where the request's values come from. The walk doesn't go up through
  dynamic calls, so it stops at a handler and not at the middleware that
  calls `next.ServeHTTP`, which VTA links to every handler.
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
one built-in list per flow template, and each sink names all of its input
arguments:

| `--sink` | Template | Examples, with input arguments |
| --- | --- | --- |
| `sql` | `sql-injection` | `(*database/sql.DB).Query(query)`, `Exec(query)`; GORM's `Raw(sql)`, `Where(query)`, `Order(value)`; sqlx and pgx |
| `command` | `command-injection` | `os/exec.Command(name, args...)`, `CommandContext(name, args...)`, `syscall.Exec(argv0, argv)` |
| `path` | `path-traversal` | `os.Open(name)`, `OpenFile(name)`, `Create(name)`, `Remove(name)`, `RemoveAll(path)`, `WriteFile(name)`, `ReadFile(name)`, `Rename(old, new)`, `Symlink(old, new)` |
| `ssrf` | `ssrf` | `(*net/http.Client).Do(req)`, `Get(url)`, `Post(url)`; `net/http.NewRequest(url)`, `net.Dial(address)` |

Three rules find the sink calls a list doesn't name, and drop the ones
that can't carry input:

- **Forwarders are sinks.** A project function that passes one of its
  parameters to a sink's input argument, directly or through a slice or a
  variadic argument, is itself a sink, and the rule applies again to its
  callers. Gogs runs most commands through `process.Exec`, `ExecTimeout`
  and `ExecDir`, which share one `exec.Command` call and have 26 callers.
  Without this rule, the 3 paths to that call site show 3 of the 26
  callers, and probably not `PullRequest.Merge`, the command-injection
  label. With it, each of the 26 calls is a sink call with paths of its
  own. Each item's header
  names the chain down to the real sink: "`ExecDir`, which calls
  `os/exec.Command`".
- **Sinks reached through dependencies count.** When a project calls a
  dependency, and that dependency calls a sink by static calls, the
  project's call is a sink call of the same kind. Gogs calls
  `git-module.Clone`, which runs `git`, so it becomes a `command` sink.
  Only static calls count inside dependencies. Interface calls would reach
  every sink. A dependency that does network work in a subprocess, as
  `git clone` does, shows up as `command`, not `ssrf`. The spike listed
  `git-module.Clone` as an SSRF sink by hand. `--sink-call` adds such a
  call to a kind by pattern.
- **Constant arguments drop.** A sink call whose input arguments are all
  constant can't carry untrusted input: `os.Open("/etc/gogs.ini")`, or
  `db.Where("id = ?", id)`, where the SQL is constant and the value is a
  parameter. `exec.Command("git", args...)` doesn't drop: its name is
  constant, but its args aren't, and the branch-name bugs that Gogs'
  fixes #8390 and #8393 were about are in the args. In SSA, a variadic
  argument is a slice; the call drops only when every value stored in it
  is constant. A sink found through a dependency has no named input
  arguments, so it never drops. The rule is local to the call. A constant
  passed down from a caller stays a path, and the model judges it, as it
  did for `orderBy`.

The [`where` proposal](check-packs.md#sequence-one-pr-each) may move the
sink lists into each `template.json`. Until then they live in
`decide-paths`.

### Building paths

The builder walks back from sinks, not forward from entry points. Forward
walks from every handler reach most of the program through shared helpers.

1. Find each call site in the project's code that calls a sink, after the
   rules above.
2. From the function that holds it, walk back through callers,
   breadth first, to an entry point, with the outermost rule.
3. Keep up to 3 paths per call site, shortest first, each from a
   different entry point when there is one. `--per` changes the number.
4. Stop at 8 functions deep. `--depth` changes it.

Test files are skipped.

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

**Size.** decide cuts a record into parts when its text, encoded as JSON,
is larger than `MaxItemBytes` less `StateRoom` and the size of its label
(`internal/source`). JSON encoding makes `<`, `>` and `&` 6 bytes each,
and tabs, newlines and quotes 2. A cut path loses the flow, so the builder
measures `json.Marshal(text)` against the same limits, imported from
`internal/source`, with room for a label. When a path is too large, it
shortens the middle functions, never the first or the last: it keeps each
one's signature and the lines around the call to the next, and writes
`// 140 lines left out`. A test checks that decide reads no record from
`decide-paths` in parts. The largest Gogs path in the spike was 28 KB.

**Labels.** Today decide labels a record by its file and line, such as
`paths.jsonl:3`, so `--format github` would annotate the JSONL file. The
`--json` output carries the whole record, so the plugin and scripts have
`source`. `--each path` labels each item by `source` (see
[Rollout](#rollout)).

### Bounds

| Bound | Default | Why |
| --- | --- | --- |
| Paths per sink call | 3 | The spike's value. With forwarders as sinks, a shared helper no longer spends them all. |
| Depth | 8 functions | The spike used 6, and its longest paths reached it, so some were probably cut. |
| Item size | under decide's limit | A cut path loses the flow. |
| Items in a run | none | `decide run --dry-run` counts them, and `--limit` caps them, as for any run. |

`decide-paths` prints its counts to stderr: the paths, the sink calls, and
the sink calls with no path from an entry point. The spike counted
by function, not call site: in Gogs it found 17 SSRF paths to 10 sink
functions, 151 command paths to 86, 124 file paths to 64, and 607 SQL
paths to 291.

### What has no path

Some flows don't start at a request, and some bugs aren't at a sink.
Function items still judge these, so path items add to function items for
the flow templates. They don't replace them.

- **Stored input.** A user saves a webhook URL or a mirror address in one
  request, and a background job uses it later. In Gogs, `HookTask.deliver`
  and `Mirror.runSync`, two of the SSRF labels, run in goroutines that
  start at boot (`go DeliverHooks()`, `go SyncMirrors()`). No caller takes
  a request, so they have no path.
- **Code that builds or checks, but doesn't call a sink.** `UserPath`
  builds a path but opens no file, and `isRepositoryGitPath` is a check.
  Both were Gogs path-traversal bugs.
- **Sinks the lists miss.** `MigrateRepository` reaches the network
  through `git-module.Clone`, which the dependency rule makes a `command`
  sink, so an `ssrf` run misses it unless `--sink-call` adds it.

`decide-paths --orphans` writes one more record per sink call that has no
path: the function that holds it, with its source. A run on paths and
orphans covers every sink call, and judges the orphans as function items
are judged today.

## Alternatives considered

- **`--each path` inside `decide`, from the start.** One command for the
  person. It links `golang.org/x/tools` into a binary that runs on any
  project, and it has the Go version problem above. It also puts one
  language's analysis inside the generic CLI before we know it works. We
  get there later by running `decide-paths`, as git runs `git-foo`.
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
  enters, so trust stays a guess. It may suit orphans better than the
  orphan's function alone.
- **Forward from entry points.** Every handler reaches most of the
  program. Backward from sinks gives fewer paths, and every one ends at a
  sink.

## Failure modes

- **A missing entry point.** A queue consumer, a webhook from another
  service, or a framework not in the table. Its paths are lost with no
  error. The counts on stderr, `--orphans` and `--entry` are the answer.
  Each new framework is one row in the table.
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

1. **`decide-paths` for Go**, with the entry and sink rules above, and
   the decide change that reads a dataset as records under a template's
   default unit. Tests on a fixture module in `testdata/`, and a planted
   path in `demo/`. Measure it against the spike on Gogs, on every label,
   not only those with a path. A label with no path counts at its rank
   in the function run, as an orphan would be judged. Compare each label's
   rank, the items flagged at 0.5, and the requests per sweep, for three
   runs: functions only, paths only, and paths plus orphans. Add a second
   Go project with labeled fixes, and one of ours to count false
   positives. It ships when paths plus orphans rank the labels at least
   as high as functions only, with fewer items flagged.
2. **The plugin** runs it for flow templates on Go projects.
3. **`--each path`.** decide runs `decide-paths` from `PATH`, passes the
   template's sinks, labels each item by its sink call, and names the unit
   `path`. This needs the [`where` proposal](check-packs.md#sequence-one-pr-each),
   so that templates name their sinks.
4. **TypeScript**, as a second builder that writes the same JSONL.

## Open questions

1. Is paths plus orphans the default for flow templates, or paths plus
   every function? The second costs more requests and may find bugs that
   aren't at a sink, such as `UserPath`. Step 1 measures both.
2. Should `decide-paths` take a template name (`--template ssrf`) instead
   of a sink kind, so the template and its sinks can't drift apart? Keep
   `--sink` until the `where` proposal decides where sinks live.
3. Should a dependency that runs a network tool in a subprocess, such as
   `git clone` with a URL, count as both `command` and `ssrf`? A short
   built-in list of such calls may be enough.
