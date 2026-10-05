# Check packs: universal, language, project

Status: direction set, spike running. Written 2026-10-04.

> Built-in templates that find known classes of vulnerability and bug,
> named by their CWE and OWASP category, before any project-specific
> setup.

## Direction

Code checks come in four scopes. We build them in this order:

1. **Universal.** Vulnerability classes that occur in any web or backend
   code, such as SQL injection or path traversal. One template per class,
   named by its CWE.
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

## What fits: visible in one function

decide judges one item at a time. A weakness fits when one function shows
it, and a call or an import finds the candidates. Phase 0 showed both
sides. The Gogs path traversal (CWE-22) and symlink (CWE-59) bugs ranked in
the top 3 of about 2,000 functions. A question about repo ownership by ID
(CWE-639) put 103 functions above 0.5, because whether a lookup needs an
owner check depends on the rest of the app.

Each template asks about one mistake, narrowly. Phase 0 found that the
wording decides recall: a question about the mistake ranks well, and a
question about its category is noisy.

## Universal pack

Categories from the [OWASP Top 10:2025](https://top10.owasp.org/2025),
ranks from the [2025 CWE Top
25](https://cwe.mitre.org/top25/archive/2025/2025_cwe_top25.html).

| Template | CWE (Top 25 rank) | OWASP 2025 | Candidates |
| --- | --- | --- | --- |
| `sql-injection` | 89 (#2) | A05 Injection | `Query`, `Exec`, `Raw`, `cursor.execute` |
| `command-injection` | 78 (#9), 77 (#23), 88 | A05 | `exec.Command`, `subprocess`, `child_process`, `Runtime.exec` |
| `path-traversal` | 22 (#6), 59 | A01 Broken Access Control | `filepath.Join`, `os.Open`, `open`, `fs.*`, `new File` |
| `ssrf` | 918 (#22) | A01 | `http.Get`, `NewRequest`, `fetch`, `requests` |
| `xss` | 79 (#1) | A05 | `template.HTML`, `innerHTML`, `dangerouslySetInnerHTML`, writers |
| `code-injection` | 94 (#10), 95 | A05 | `eval`, `new Function`, `exec` |
| `unsafe-deserialization` | 502 (#15) | A08 Integrity Failures | `pickle`, `yaml.load`, `ObjectInputStream` |
| `weak-crypto` | 327, 328, 330, 295 | A04 Cryptographic Failures | `md5`, `sha1`, `DES`, `math/rand`, `InsecureSkipVerify` |
| `secrets` | 798 | A07 Authentication Failures | a text pattern and entropy |
| `file-upload` | 434 (#12) | none | multipart handlers |
| `error-handling` | 252, 390, 636, 703, 754, 755 | A10 Mishandling of Exceptional Conditions | `err`, `catch`, `except` |
| `unbounded-resource` | 770 (#25), 400 | none | `io.ReadAll`, `make`, `http.Client` |
| `sensitive-logging` | 532, 200 (#20) | A09 Logging and Alerting Failures | `log.*`, `logger.*` |

The [OWASP Top 10 for LLM
Applications](https://genai.owasp.org/llm-top-10/) adds two that fit
decide's audience: `prompt-injection` (LLM01) already ships, and model
output passed to a shell, a query or HTML (LLM05, improper output
handling) is a candidate.

## Go pack: what linters can't see

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
can be quieter. decide also covers frameworks no one wrote rules for. Each
finding carries its CWE, so it can go into SARIF and GitHub code scanning
beside the other tools.

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
  vulnerable functions near the top.
- **Our own Go repositories,** for the Go pack: read the top of each
  ranking, and compare with `golangci-lint` on the same code.

## Spike

No code changes. Write five templates and measure them:

1. `sql-injection`, `command-injection`, `path-traversal` and `weak-crypto`
   on the Benchmark's cases in their categories.
2. `command-injection` and `path-traversal` on Gogs before the 2026 fixes.
3. `go-context`, `go-goroutine-leak`, `go-error-swallowed` and
   `go-typed-nil` on dive, read by hand beside `golangci-lint`.

Results go in this document.

## Sequence (one PR each, after the spike)

1. The templates that pass the spike, as built-ins, with planted cases in
   `demo/` and labeled examples.
2. A proposal for `where` in `template.json`. Packs need it more than
   hunts: it removes cost, and it keeps unrelated code from getting a score.
3. A proposal for CWE and OWASP tags in `template.json`, carried into
   `--format github` and SARIF. Until then, each template's README names
   them.
4. `decide eval`, with each built-in's labeled examples.
5. The Go pack, then the TypeScript pack.
6. Deviance templates for the project checks.
