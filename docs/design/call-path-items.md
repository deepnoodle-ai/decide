# Call-path items

Status: proposal, with a spike on Gogs. Written 2026-10-04.

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
| `ssrf` | `ssrf` | `(*net/http.Client).Do(req)`, `Get(url)`, `Post(url)`, `Head(url)`; `net/http.Get(url)`, `Post(url)`; `net.Dial(address)` |

A sink is the call that acts: it connects, runs or opens. Calls that only
build the input, such as `http.NewRequest`, are not sinks; the request
reaches `Client.Do`, and the path ends there.

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
5. Don't walk up through a function that takes no input: no parameters,
   no receiver and no captured variables. Its callers can't pass it a
   request's values. Don't walk up across a `go` statement either. In the
   spike, without this rule, Gogs' install handler reached the webhook
   goroutine: `InstallPost -> GlobalInit -> InitDeliverHooks ->
   DeliverHooks -> HookTask.deliver`.

Test files are skipped. Everything is sorted by name and by file and
offset, never by map order or `token.Pos`, which changes between runs
because packages load in parallel. Two runs on the same code write the
same bytes, so the answer cache holds. A test checks it.

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
shortens functions in this order until the text fits: the middle ones,
then the one that holds the sink call, then the entry point. Each keeps
its signature and the lines around its call to the next function, or
around the sink call, and writes `// 140 lines left out`. An orphan keeps
the lines around its sink call. If the text still doesn't fit, the
builder skips the item and counts it on stderr. It never writes an item
that decide would cut. Tests cover a one-function path, a path whose
entry point or sink function is too large alone, and a check that decide
reads no record from `decide-paths` in parts. The largest Gogs path in
the check-packs spike was 28 KB, and no item in this spike needed
shortening.

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
the sink calls with no path from an entry point. On Gogs, the spike built
1,587 items for the four templates, against 8,120 function items. The
largest was 22 KB, and none needed shortening.

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
  sink, so an `ssrf` run misses it unless `--sink-call` adds it. The
  spike lost `MigrateRepository` and `Mirror.runSync` this way.

`decide-paths --orphans` writes one more record per sink call that has no
path: the function that holds it, with its source. A run on paths and
orphans covers every sink call, and judges the orphans as function items
are judged today. It doesn't cover the code above, which has no sink
call: 6 of the spike's 13 real labels.

## Spike results

[`spikes/call-paths`](../../spikes/call-paths) builds items by these
rules, and ran the four flow templates on Gogs before the 2026 fixes
(`199cf4fd5^`), with paths and orphans. The function run is the
[check-packs spike's](check-packs.md#gogs-before-the-2026-fixes), with the
same templates. A path's rank counts sink-call functions, each at the
best score of its paths, so it is the number a reader goes through before
reaching the label.

| Label | Fix | By function, of 2,030 | Paths plus orphans | Without the dependency rule |
| --- | --- | --- | --- | --- |
| `PullRequest.Merge` | #8301 command | 4 | 4 of 105 | 3 of 19 |
| `CompareAndPullRequestPost` | #8390 command, in `git-module` | 56 | 25 | 17 |
| `getRepoGitTree` | #8393 command, in `git-module` | 390 | 28 | no sink call |
| `getArchive` | #8393 | 94 | 30 | no sink call |
| `getContents` | #8393 | 264 | 60 | no sink call |
| `Request.getResponse` | #8263 SSRF | 22 | 1 of 3, an orphan | 1 of 1 |
| `HookTask.deliver` | #8263 | 50 | no sink call | |
| `MigrateRepository` | #8324 SSRF | 2 | lost: `git clone` is `command` | |
| `Mirror.runSync` | #8324 | 30 | lost: `git fetch` is `command` | |
| `UploadRepoFiles` | #8332 path | 41 | 19 of 245 | 17 of 69 |
| `UserPath` | #8334 | 16 | no sink call | |
| `RepoPath` | #8334 | 65 | no sink call | |
| `isRepositoryGitPath` | #8408 | 194 | no sink call | |
| `GetByCollaboratorID` | false positive, SQL | 1 | not judged | not judged |
| `searchUserByName` | false positive, SQL | 3 | not judged | not judged |

| Template | Function items | Flagged at 0.5 | Path and orphan items | Sink-call functions flagged | Without the dependency rule |
| --- | --- | --- | --- | --- | --- |
| `ssrf` | 2,030 | 45 | 10 | 1 | 1 item, 1 flagged |
| `command-injection` | 2,030 | 25 | 269 | 64 | 37 items, 9 flagged |
| `path-traversal` | 2,030 | 54 | 617 | 47 | 188 items, 31 flagged |
| `sql-injection` | 2,030 | 6 | 691 | 2 | 134 items, 2 flagged |

- **Paths rank flows higher.** Every label with a sink call ranked as
  high as by function, or higher. The #8393 API handlers moved the most,
  `getRepoGitTree` from 390 to 28. As a function, it passes a URL
  parameter to `gitRepo.LsTree(sha)`. As a path, the header says that call
  leads to `os/exec.CommandContext`.
- **The dependency rule finds the bugs in libraries.** #8390 and #8393
  were fixed in `git-module`, and only the dependency rule makes Gogs'
  calls into it sink calls. It costs 4.4 times the items (360 to 1,587),
  and adds noise: every xorm query is a SQL sink call, and every
  `git-module` call is a command sink call. Most of that noise scores
  low, but 64 command sink-call functions were flagged.
- **Forwarders and constants removed both SQL false positives** before
  any model call. `GetByCollaboratorID` and `searchUserByName` forward
  `orderBy`, so their callers hold the sink calls, and every caller
  passes a constant. The same rules found `Merge` through
  `process.ExecDir`.
- **Function items are still needed.** 6 of 13 real labels have no sink
  call for their template: stored input (`HookTask.deliver`), code that
  builds or checks a path (`UserPath`, `RepoPath`, `isRepositoryGitPath`),
  and network calls
  made by `git` (`MigrateRepository`, `runSync`).
- **Setup handlers are trusted input that looks untrusted.** Gogs'
  `InstallPost` takes the install form and calls `GlobalInit`, which
  starts the server. Its paths were 4 of the top 5 for path traversal, at
  0.87 to 0.93. The rule in step 5 cut install paths from 35 to 14, but
  `GlobalInit` takes a parameter, so the rest stay.
- **The builder is cheap.** About 4 seconds and 2.9 GB of memory per
  template on Gogs, most of it loading packages and building SSA.

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
- **Setup handlers.** An install or setup form is filled in by the
  operator, but it arrives as a request, so its paths look untrusted. See
  [Spike results](#spike-results) and open question 4.
- **Large projects.** SSA and VTA for a large module take time and memory:
  4 seconds and 2.9 GB for Gogs. We measure one larger Go project before
  release.

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
   path in `demo/`. The spike measured Gogs. Before release, measure a
   second Go project with labeled fixes, and one of ours to count false
   positives, on every label, as in [Spike results](#spike-results). It
   ships when every label with a sink call ranks at least as high as by
   function, with fewer items flagged than the function run.
2. **The plugin** runs it for flow templates on Go projects.
3. **`--each path`.** decide runs `decide-paths` from `PATH`, passes the
   template's sinks, labels each item by its sink call, and names the unit
   `path`. This needs the [`where` proposal](check-packs.md#sequence-one-pr-each),
   so that templates name their sinks.
4. **TypeScript**, as a second builder that writes the same JSONL.

## Open questions

1. Paths plus orphans missed 6 of the spike's 13 real labels, which function
   items find. So flow templates run both, for now. How do we show the
   two lists? The path list ranks better, so it could come first, with
   function items that have no sink call after it.
2. Should `decide-paths` take a template name (`--template ssrf`) instead
   of a sink kind, so the template and its sinks can't drift apart? Keep
   `--sink` until the `where` proposal decides where sinks live.
3. Should a dependency that runs a network tool in a subprocess, such as
   `git clone` with a URL, count as both `command` and `ssrf`? The spike
   lost `MigrateRepository`, rank 2 by function, and `runSync` for want
   of it. A short built-in list of such calls may be enough.
4. How do we treat setup handlers? Options: the trust wording names an
   install or setup form as the operator's, `--skip-entry`, or a rule
   that skips handlers that call the code `main` runs at startup. The
   last one needs no setup, but it is the hardest to get right.
5. Is the dependency rule worth 4.4 times the items? It found the
   `git-module` bugs, and nothing else did. It could apply only to
   `command`, where those bugs were, or only to dependencies that aren't
   database drivers.
6. With one `security` template that asks every class in one request
   ([check-packs.md](check-packs.md#one-request-per-item)), how does a
   path item meet it? Either every path is asked every question, and the
   flag that counts is the one for the path's `kind`, or `decide-paths`
   builds the paths for all of a pack's sinks in one run and decide asks
   each path only its kind's question. The first needs no change to
   decide.
