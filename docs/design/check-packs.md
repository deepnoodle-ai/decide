# Check packs: universal, language, project

Status: direction set, spike done. Written 2026-10-04.

> Built-in templates that find known classes of vulnerability and bug,
> named by their CWE and OWASP category, before any project-specific
> setup.

## Direction

Code checks come in four scopes. We build them in this order:

1. **Universal.** Vulnerability classes that occur in any web or backend
   code, such as SQL injection or path traversal. One template per pack,
   with one question per class, named by its CWE.
2. **Language.** Mistakes that one language makes easy, such as an
   unclosed `resp.Body` in Go or a missing `await` in TypeScript.
3. **Project.** A project's own conventions, found by deviance: if 18 of
   20 handlers check admin access, judge the 2 that don't.
4. **Bug.** One fixed bug's siblings. `/decide:hunt` does this, and it
   shipped in #52. See [fix-one-find-all.md](fix-one-find-all.md).

The [SDLC brainstorm](sdlc-brainstorm.md) led with the fourth. Universal
checks come first now. They work with no setup and on the first run, and
standard names (CWE, OWASP) let their findings slot into the reports teams
already keep.

## The unit: a function, or a call path

decide judges one item at a time, and the item is our choice. So far it
has been a function. A weakness fits when the item shows it. Phase 0
showed both sides. The Gogs path traversal (CWE-22) and symlink (CWE-59)
bugs ranked in the top 3 of about 2,000 functions. A question about repo
ownership by ID (CWE-639) put 103 functions above 0.5, because whether a
lookup needs an owner check depends on the rest of the app.

The spike found that most false positives come from the unit, not the
question. A function that passes a parameter to `Order()` can't show that
every caller passes a constant. A Benchmark test case that reads a value
through a helper class can't show that the helper returns `"bar"`. A
**call path** can: the entry point that receives the request, each
function down to the one that calls the sink, and their source, in one
item. Adding the called code raised every Benchmark score that used it
(see [Spike results](#spike-results)), and a path from a Gogs route
handler cut one `orderBy` false positive from 0.90 to 0.33. Paths that
start from sinks can also cut the number of items: 2,030 functions became
17 SSRF paths, 124 file paths, 151 command paths and 607 SQL paths. Each
path is larger than a function, up to 28 KB in the spike.

So the unit is a function by default, and a call path from an entry point
to a sink where the bug is a flow. A deterministic tool builds the paths;
decide judges them. An item holds up to 64 KB before decide splits it, and
the deepest Gogs path was 28 KB.

Each question asks about one mistake, narrowly, and a pack asks all its
questions in one request per item (see [One request per
item](#one-request-per-item)). Phase 0 found that the
wording decides recall: a question about the mistake ranks well, and a
question about its category is noisy. The spike found the same for trust:
"input from outside the program" counted flags and config files as attacker
input. "Untrusted input", with the operator's flags and config named as
trusted, cut flags on Gogs by 15% to 40% and left Benchmark scores within
0.01. It has a cost: the labeled SSRF bug in `Request.getResponse` fell
from rank 4 to 22.

## Universal pack

Categories from the [OWASP Top 10:2025](https://top10.owasp.org/2025),
ranks from the [2025 CWE Top
25](https://cwe.mitre.org/top25/archive/2025/2025_cwe_top25.html).

| Class | CWE (Top 25 rank) | OWASP 2025 | Candidates |
| --- | --- | --- | --- |
| `sql-injection` | 89 (#2) | A05 Injection | `Query`, `Exec`, `Raw`, `cursor.execute` |
| `command-injection` | 78 (#9), 77 (#23), 88 | A05 | `exec.Command`, `subprocess`, `child_process`, `Runtime.exec` |
| `path-traversal` | 22 (#6), 59 | A01 Broken Access Control | `filepath.Join`, `os.Open`, `open`, `fs.*`, `new File` |
| `ssrf` | 918 (#22) | A01 | `http.Get`, `NewRequest`, `fetch`, `requests` |
| `xss` | 79 (#1) | A05 | `template.HTML`, `innerHTML`, `dangerouslySetInnerHTML`, writers |
| `code-injection` | 94 (#10), 95 | A05 | `eval`, `new Function`, `exec` |
| `unsafe-deserialization` | 502 (#15) | A08 Integrity Failures | `pickle`, `yaml.load`, `ObjectInputStream` |
| `weak-crypto` | 327, 328, 330; 295 | A04 Cryptographic Failures; 295 is A07 Authentication Failures | `md5`, `sha1`, `DES`, `math/rand`, `InsecureSkipVerify` |
| `secrets` | 798 | A07 Authentication Failures | a text pattern and entropy |
| `file-upload` | 434 (#12) | A06 Insecure Design | multipart handlers |
| `error-handling` | 252, 390, 636, 703, 754, 755 | A10 Mishandling of Exceptional Conditions | `err`, `catch`, `except` |
| `unbounded-resource` | 770 (#25), 400 | none | `io.ReadAll`, `make`, `http.Client` |
| `sensitive-logging` | 532; 200 (#20) | A09 Logging and Alerting Failures; 200 is A01 | `log.*`, `logger.*` |

The [OWASP Top 10 for LLM
Applications](https://genai.owasp.org/llm-top-10/) adds two that fit
decide's audience: `prompt-injection` (LLM01) already ships, and model
output passed to a shell, a query or HTML (LLM05, improper output
handling) is a candidate.

## Go pack: judgment, not proof

Most Go mistakes that compile and pass review are found exactly by
`golangci-lint` with `errcheck`, `errorlint`, `ineffassign`, `staticcheck`,
`bodyclose`, `sqlclosecheck`, `govet` (`copylocks`, `lostcancel`, `printf`,
`structtag`, `shadow` on `err`) and a narrow `gosec` (G101, G201, G202,
G402, G404). decide doesn't repeat them. Following the brainstorm's
"deterministic first" rule, the Go pack covers what those tools only partly
see, or can't:

| Template | Mistake | CWE |
| --- | --- | --- |
| `go-context` | A context or timeout that never reaches the call that blocks: `Query` instead of `QueryContext`, `http.NewRequest` instead of `NewRequestWithContext`, a goroutine that ignores `ctx` | 400 |
| `go-goroutine-leak` | A goroutine that can block forever: a send with no receiver left, a `select` with no `ctx.Done()` | 772, 401 |
| `go-typed-nil` | A typed nil pointer returned as an `error` or other interface, so `== nil` is false | 476 |
| `go-append-alias` | A slice passed to a function that appends to it, where the caller keeps the old header | 664 |
| `go-error-swallowed` | An error turned into success: logged and dropped, replaced by a default, or returned as `nil` after a failure. Linters see `_ = err`, not these | 390, 636 |

The bugs that linters can't see, and that a deep analyzer needs
interprocedural, alias-aware analysis to prove, sort by whether one item
shows their core:

- **Strong fit:** the item shows it, and seeing it is a judgment. A parent
  that starts N workers and returns on the first error, leaving the rest
  blocked on a send; a `select` that can only block; a context that never
  reaches the goroutine or call it was made for; an error that is logged
  and then followed by use of the zero value; a deferred `Close` whose
  error is lost on a write path; a cache keyed by request with no bound;
  a multi-step write with no rollback. Analyzers call the last two
  research, because they need a spec the code doesn't contain. A model
  supplies that spec as common sense, which makes them decide's
  distinctive checks.
- **Partial fit:** the item shows the shape, not the proof. Double
  close, `WaitGroup` counts across paths, mutex paths, resources stored
  and never closed, `append` aliasing, library protocols such as
  `sql.Rows`, `bufio.Writer` and tickers. Call-path items help here.
- **Poor fit:** a whole-program or cross-goroutine view. Lock order,
  data races, nil flow across many calls. `go test -race`, GCatch and
  NilAway own these.

Partial-failure semantics (an error followed by the zero value, siblings
abandoned on the first error, no rollback) aren't specific to Go. They
belong in the universal pack under A10.

Linter output can also be items: judge each `errcheck` or `gosec` finding
for "is this a real problem here?" and rank them. That is optional, as the
brainstorm says for SARIF.

TypeScript comes next: a missing `await` and an unhandled rejection
(CWE-755), and prototype pollution (CWE-1321).

## Project checks

Authorization weaknesses rank high in the Top 25: missing authorization
(CWE-862, #4), incorrect authorization (CWE-863, #17), access control
(CWE-284, #19), bypass through a user-controlled key (CWE-639, #24),
missing authentication (CWE-306, #21) and CSRF (CWE-352, #3). They are A01
and A07. One function can't show whether they are wrong, so they are
deviance templates: `where` selects a peer group, such as the handlers
under `admin/`, and the question asks for the convention or a reason
without it.

## Not a fit

- **Memory safety** (CWE-787, 416, 125, 120–122). decide's scanners don't
  read C or C++, and sanitizers and fuzzers do this better.
- **A03 Software Supply Chain Failures.** `govulncheck` and `npm audit`
  answer it exactly; decide could rank their output.
- **A02 Security Misconfiguration.** It lives in config files, not
  functions. A later item type, perhaps.
- **A06 Insecure Design.** Not visible one function at a time.

## What decide adds over scanners

gosec, Semgrep and CodeQL find most of the universal pack by syntax. Their
weakness is context: whether a value really comes from input, and whether
it was sanitized on the way. That is a judgment, and it is where decide
can be quieter. decide also covers frameworks no one wrote rules for.
Today `--format github` writes annotations on a pull request. Once
templates carry CWE tags and decide writes SARIF (sequence step 4), each
finding can go into GitHub code scanning beside the other tools.

## Measure

- **[OWASP Benchmark](https://owasp.org/www-project-benchmark/) 1.2
  (Java).** 2,740 test cases, each labeled real or not, by CWE: command
  injection, SQL injection, path traversal, XSS, weak cipher, weak hash and
  weak random values, among others. Its score is the true-positive rate
  minus the false-positive rate. It has been public since 2016, so models
  may have seen it; treat it as an upper bound.
- **Gogs CVEs from 2026.** Phase 0's clusters cover CWE-88, 22, 59 and
  639, and their fixes are newer than the model's likely training data.
  Universal templates, written without seeing the fixes, should rank the
  vulnerable functions near the top. (The spike's second wording was
  tuned after reading the first Gogs results, so its Gogs ranks are not
  blind.)
- **Our own Go repositories,** for the Go pack: read the top of each
  ranking, and compare with `golangci-lint` on the same code.

## Spike results

Run 2026-10-04 with decide from `main` (Jev, `jev-1.13.0`), no code
changes. The templates, the call-path tool and scripts to rerun and
score it all are in
[spikes/check-packs](../../spikes/check-packs/). Each template asks one `noul` question per item; `weak-crypto`
asks four.

### OWASP Benchmark 1.2

Each test case is one item: its file, and in the second run, the source of
the helper classes it calls (`SeparateClassRequest`, `ThingFactory` and its
`Thing` classes) and `benchmark.properties`. The score is the true-positive
rate minus the false-positive rate. "Best" picks the threshold for each
category on the same data, so it is optimistic; "at 0.5" uses one fixed
threshold. AUC is the chance that a real case scores above a safe one.

| Category (CWE) | Cases | File only, at 0.5 | File only, best | Called code, at 0.5 | Called code, best | AUC, called code |
| --- | --- | --- | --- | --- | --- | --- |
| SQL injection (89) | 504 | 0.48 | 0.68 | 0.65 | **0.84** | 0.91 |
| Command injection (78) | 251 | 0.41 | 0.66 | 0.62 | **0.78** | 0.92 |
| Path traversal (22) | 268 | 0.64 | 0.73 | 0.78 | **0.81** | 0.90 |
| XSS (79) | 455 | 0.69 | 0.78 | 0.83 | **0.90** | 0.95 |
| Weak cipher (327) | 246 | 0.57 | 1.00 | 0.59 | **1.00** | 1.00 |
| Weak hash (328) | 236 | 0.61 | 0.68 | 1.00 | **1.00** | 1.00 |
| Weak random (330) | 493 | 1.00 | 1.00 | 1.00 | **1.00** | 1.00 |

The gap between the two thresholds is the case for `decide eval`: the
weak-cipher question separates every case (AUC 1.00) but needs a
threshold near 0.95, not 0.5. Each built-in needs its threshold measured.

- Recall was 1.00 at 0.5 in every injection category. The errors are
  false positives.
- With the file only, the false positives split two ways: a helper in
  another file returns a constant (decide can't see it), and data-flow
  puzzles, such as a list where the input is added and then removed. The
  called code fixes the first. The second remains.
- The weak-hash misses were all a hash algorithm read from
  `benchmark.properties`. With the file in the item, they resolved.
- The Benchmark has been public since 2016, so these are an upper bound.

### Gogs, before the 2026 fixes

One snapshot before all of these fixes (`199cf4fd5^`, 2,030 functions).
The labels are the functions each fix changed. #8390 and #8393 were fixed
in the `git-module` dependency, so they have no label in Gogs.

| Template | Labeled function (fix) | Rank by function | Rank by call path |
| --- | --- | --- | --- |
| `ssrf` | `MigrateRepository` (#8324) | 2 of 2,030 | 4 of 10 sinks |
| `ssrf` | `Request.getResponse` (#8263) | 22 | 3 of 10 |
| `ssrf` | `Mirror.runSync` (#8324) | 30 | 6 of 10 |
| `ssrf` | `HookTask.deliver` (#8263) | 50 | 7 of 10 |
| `command-injection` | `PullRequest.Merge` (#8301) | 4 of 2,030 | 3 of 86 sinks |
| `path-traversal` | `UserPath` (#8334) | 16 | no path: it builds a path, it doesn't open one |
| `path-traversal` | `UploadRepoFiles` (#8332) | 41 | 27 of 64 |
| `path-traversal` | `isRepositoryGitPath` (#8408) | 194 | no path |

- **SSRF and command injection look promising, on few labels:** four SSRF
  labels and one command-injection label, in one repository.
  `HookTask.deliver` scored 0.47, under 0.5. The top 15 command-injection
  functions also hold the branch-name callers (`CheckoutNewBranch`,
  `UpdateLocalCopyBranch`, `CreateNewBranch`) that #8390 and #8393 were
  about.
- **Path traversal doesn't, yet.** Its top results are install and
  restore commands that take paths from the operator, and the real bugs
  are subtle: a symlink in a parent directory, `.git ` with a trailing
  space on Windows. It needs better wording or a narrower question per
  shape.
- **SQL injection's top hits were false positives with one cause:**
  `GetByCollaboratorID` and `searchUserByName` pass `orderBy` to GORM's
  `Order()`, and every caller passes a constant. By call path,
  `GetByCollaboratorID` fell from 0.90 to 0.33. `searchUserByName` stayed
  high because the spike's path builder stopped one call too early; it
  treated every function in `internal/route` as an entry point.
- **Path bounds.** The path builder kept up to 3 paths per sink function
  and 6 functions deep. Its longest paths reached 6, and 123 of the 291
  SQL sink functions had 3 paths, so both limits cut paths.
- **One request, four questions.** One template with all four questions
  (`templates-combined/universal`) asked them in 2,030 requests instead
  of 8,120. The answers moved by 0.004 to 0.006 on average and never by
  more than 0.2. 29 of each top 30 were the same (25 for SQL), and the
  flagged counts stayed within 3. Labels moved a few places either way:
  `MigrateRepository` 2 to 1, `UserPath` 16 to 26, `UploadRepoFiles` 41
  to 33. Nine questions did nearly as well; see the next section.
- **Trust wording.** Naming operator flags and config as trusted cut the
  functions flagged at 0.5 from 81 to 54 for path traversal, 32 to 25 for
  command injection, 53 to 45 for SSRF and 10 to 6 for SQL injection.

### One request per item

The security pack (`templates-combined/security`) asks nine questions in
one request: the four above, `xss`, and `weak-crypto`'s cipher, hash,
random and TLS questions. Its answers match one template per question:

| Run | Mean change | Largest change | Rank correlation | Top 30 shared |
| --- | --- | --- | --- | --- |
| Gogs, SSRF | 0.006 | 0.10 | 0.96 | 28 |
| Gogs, command injection | 0.004 | 0.12 | 0.90 | 28 |
| Gogs, path traversal | 0.004 | 0.09 | 0.92 | 27 |
| Gogs, SQL injection | 0.004 | 0.10 | 0.97 | 24 |

On the Benchmark, file only, every category's AUC stayed within 0.01 and
its score at 0.5 within 0.03; the best score for command injection fell
from 0.66 to 0.63. The flagged counts on Gogs stayed within 3. Each label
moved 10 places or fewer, except `isRepositoryGitPath`, 194 to 221.

So a pack is one template, and one request per item: 2,030 requests for
Gogs instead of 18,270. We split a pack only when a measurement shows a
cost. The backend allows 64 questions in a request, and we measure again
as a pack grows.

The new questions on Gogs at 0.5:

- `xss` flagged 179 functions. Its top hits are Markdown and webhook
  renderers that build HTML, but most of the 179 are route handlers (88)
  and database functions (47). At 0.7 it flags 36. It needs its threshold
  measured on Go labels.
- The TLS question flagged 8: login sources (LDAP and SMTP), webhooks and
  hooks that obey an admin's "skip TLS verify" setting.
- The hash question flagged 6, and the cipher and random questions none.

A pack and call paths fit together, with one question still open. A
path item is built for one sink kind. Asked the whole pack, it costs no
more requests, and its `kind` names the question that matters. See open
question 6 in [call-path-items.md](call-path-items.md#open-questions).

### dive, Go pack

2,727 functions in dive at `cae698f`, beside `golangci-lint` with
`errcheck`, `errorlint`, `staticcheck`, `bodyclose`, `sqlclosecheck`,
`contextcheck`, `govet`, `gosec`, `nilnil` and `nilerr` (146 issues).

| Template | Flagged at 0.5 | Files that also have a lint issue | Top 10, verified |
| --- | --- | --- | --- |
| `go-error-swallowed` | 103 | 16 of 63 | 1 real, 6 minor, 3 false |
| `go-context` | 30 | 9 of 19 | 2 real, 4 minor, 4 false |
| `go-goroutine-leak` | 7 | 2 of 6 | 1 real, 3 minor, 6 false |
| `go-typed-nil` | 7 | | none above 0.6; the top hits are `Unwrap` methods |

decide and the linters mostly flag different code. An agent read each of
the top 10 with its callers and callees. Of 30, 4 were real: a CONNECT
proxy whose tunnels outlive `Stop`, an OAuth wait with no `ctx.Done()`, a
10-minute wait that ignores cancel, and a base64 error sent on as an empty
file part. Three of the four are in `experimental/`.

- **Not ready to ship.** The scores barely separate intended best-effort
  code from real swallowed errors (0.81 to 0.85 for both), and the leak
  question flagged channels that hold the one value they get.
- 6 of the top 30 were in `examples/` or `demos/`, and none was real. A
  default `--exclude` for those would help any Go sweep.
- The leak and context bugs that were real needed the callers to confirm,
  which is the case for call paths again.

## Sequence (one PR each)

1. A `security` built-in that asks the `sql_injection`,
   `command_injection`, `ssrf`, `xss` and four weak-crypto questions in
   one request, with the trust wording from the spike, planted cases in
   `demo/` and labeled examples. It works per function today. Before
   shipping, measure each question on Go and TypeScript labels, not only
   the Java Benchmark, and set its threshold from data. The template's
   README names each question's CWE and OWASP category, and keeps the
   spike's numbers.
2. A proposal for call-path items: [call-path-items.md](call-path-items.md).
   A separate `decide-paths` command builds Go paths from request handlers
   to sinks, and decide judges them as JSONL records.
3. A proposal for `where` in `template.json`. With paths, sinks become the
   prefilter: each question names its sinks, and only code that reaches one
   is asked about.
4. A proposal for CWE and OWASP tags on each question, carried into
   `--format github` and SARIF. Until then, the template's README names
   them.
5. `decide eval`, with each built-in's labeled examples.
6. `path_traversal` in the pack, once call paths or better wording lift
   it.
7. The rest of the universal pack, as questions in the same template,
   each measured the same way: `secrets`,
   `code-injection`, `unsafe-deserialization`, `file-upload`,
   `error-handling`, `unbounded-resource` and `sensitive-logging`.
8. The Go pack, once its questions separate real bugs from best-effort
   code (4 of 30 real in the spike), then the TypeScript pack.
9. Deviance templates for the project checks.
