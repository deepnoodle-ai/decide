# Brainstorm: decide across the SDLC in Claude Code

Status: ideas, not commitments. Written 2026-10-03 after a review of the
plugin at 0.2.1, and updated 2026-10-04 after the reply check was removed
(#53) and the command check narrowed (#54). Two independent critiques
shaped it: one of the SDLC ideas, and one of the hunting and scale
sections. Items that land move to [roadmap.md](../roadmap.md).

## Where the plugin stands

The plugin has two checks, each on a moment where Claude's own judgment
slips:

| Check | Hook | Template | Action |
| --- | --- | --- | --- |
| Command | PreToolUse on Bash and Monitor, in bypass mode only | `command-risk`'s `severe` | ask |
| Content | PostToolUse on web, MCP, `gh` and `curl` | `prompt-injection` | warning and toast |

Beside the checks it has the judge tool, the `using-decide` skill,
`/decide:hunt`, `/decide`, footer counts and verdicts on tool rows. It fails open, keeps its
runs apart from the person's own, and pins its thresholds to the templates
with a Go test.

A third check, on Claude's reply at Stop, was removed in #53. It flagged
about one reply in five, nearly all honest: most passed on a subagent's
results, which the check could not see. Ideas below that judge a whole
turn at Stop face the same problem.

It is a good foundation. Its limit is that the checks are hard-coded:
every idea below needs either a new template, a new hook binding, or both.

## The lens: what decide is uniquely good at

An idea belongs here only if it uses at least one of these:

1. **An independent second opinion.** A different model judges Claude, so
   Claude is not grading its own work.
2. **Typed answers with probabilities.** Code acts on them with a threshold:
   ask, deny, show a band, or let it pass. Raw probabilities need fitting
   for each use. `patterns/calibrate` does that from labeled examples, so
   thresholds come from data, not from a guess.
3. **Volume.** Cheap enough to run on every action, or on 200 items at once.
4. **One policy everywhere.** The same template runs in a hook, the CLI,
   pre-commit and CI.

Shapes that fail the lens, and what to do instead:

- **Needs generation.** Release notes, threat models and QA plans are prose.
  Claude writes; decide ranks or tags what Claude wrote.
- **Needs whole-repo context.** One item can't hold the repository. Some
  questions, such as coverage, import rules and API breaks, have exact
  answers: use `go test -cover`, `go list` and `apidiff`. For the others, a
  deterministic tool builds each item's context, such as a call graph or a
  route table, and decide judges the item. Context building happens outside
  decide.
- **Deterministic first, then decide.** A regex, parser or metadata lookup
  finds the candidates cheaply. Decide judges only the ambiguous ones, such
  as "a real credential or a test fixture?" This shape removes the noise
  that makes scanners hated.
- **"Claude could do this itself."** Style match and TODO triage gain
  nothing from a network hop.

## Headline: decide catches Claude cheating on its tests

Agents under pressure to make tests pass sometimes make the tests pass
instead of making the code work. They loosen an assertion, add `t.Skip`,
hardcode the expected value, swallow the error or add `|| true`. Users
complain about this widely, Claude cannot credibly police it on itself, and
the item is small: one edit hunk in a test file, or near error handling.

- **Template `test-tampering`:** `weakens_test`, `skips_test`,
  `hardcodes_expected`, `swallows_error`. Each is a `noul`.
- **Binding:** PostToolUse on Edit and Write, limited to test paths and
  changed error handling. It warns by default; ask comes later, once `decide
  eval` clears a bar.
- **Demo:** plant cases in `demo/`. The video is 20 seconds: "Claude
  loosened the assertion. decide flagged it, and Claude put it back."

## Platform moves (multipliers)

**P1. Checks as config.** `.decide/agent.json` binds a hook event, tool
matcher and path glob to a template, a threshold and an action (`warn`,
`band`, `ask`, `deny`). The existing checks become the default config. Build it
after about five checks exist, so real cases shape the schema. The schema
lives in the plugin, never in the root package or the template format.

**P2. Your CLAUDE.md, enforced.** Claude compiles CLAUDE.md or AGENTS.md
into one `noul` question per rule that can be judged, once, as an ordinary
template the user reviews and commits. Decide then judges edits, commands
and replies against it through P1. Every Claude Code user has watched it
ignore an instruction, so the tagline sells itself. Rules that a check
already covers ("never push to main") stay with that check.

**P3. Learn from overrides.** When the user runs a flagged command,
dismisses a band or acts on a warning, save it as a labeled example. It
feeds `decide eval` (on the roadmap) and per-repo threshold suggestions. Not
flashy, but it is the moat: the checks get quieter and sharper in each repo.

**P4. Suggested allow-rules, not auto-allow.** Turning `command-risk` from a
brake into an accelerator flips its failure mode: a confident false negative
would run `rm` with no prompt. Instead, decide proposes allow rules for
`settings.json` from commands it judged safe many times, and the person
accepts them once. This sits close to "Not planned: running shell commands
chosen from an answer", so propose the design first. Since #54 the command
check runs only in bypass mode, where allow rules don't matter, so P4 needs
another source of judged commands, such as a sample outside bypass mode.

**P5. CI parity.** "This will fail the decide gate in CI" before a push.
Defer it until the GitHub Action ships.

**P6. `/decide ship`.** A pre-PR battery over the branch diff:
`pr-description`, `code-risk`, `test-tampering`, a changelog gap and docs
drift. It gives one table and a verdict.

## Ideas by phase

Each idea names its hook and template. ★ marks the strongest. ⚙ marks ideas
that need a deterministic pre-filter.

### 1. Discovery and requirements (PM)

- ★ **Issue readiness on pickup.** When Claude reads an issue with `gh issue
  view`, run `task-readiness`. If the issue isn't ready, tell Claude what's
  missing before it starts guessing. It uses an existing template.
- **Acceptance-criteria traceability at Stop.** For each criterion in the
  PRD or issue, is there evidence in this turn's tools? "3 of 7 criteria
  have no evidence." Use the judge tool, with one item per criterion.
- **PRD lint.** Score each requirement for testability and measurability,
  through `decide run` over the PRD's sections.
- **Feedback mining.** Route, cluster and score sentiment over exported
  support tickets or interviews with the judge tool. It uses `sentiment` and
  `ticket-routing`.

### 2. Planning and design

- ★ **Plan review on ExitPlanMode.** Score the plan against the user's
  original request. Does it skip anything they asked for? Does it lack a
  test plan or a rollback? Does it touch migrations or the public API? A
  band shows the gaps. It runs once per plan, so it is cheap.
- ⚙ **Dependency vetting.** On `go get`, `npm install` or `pip install`,
  registry metadata comes first: age, downloads and install scripts. Decide
  then judges whether the name looks like a typosquat and whether the
  install script looks benign.
- **Plan size → fan-out.** Score the plan's size and independence, and
  suggest worktrees or `/orchestrate` for a large plan that splits cleanly.

### 3. Implementation (in the loop)

- ★ **Test tampering.** The headline, above.
- ★ **Error swallowing.** `_ = err`, an empty `catch`, or a returned nil
  error that hides a failure, judged for each changed function. It is the
  sibling of test tampering and easy to plant in the demo.
- **Stubs and placeholders.** "TODO: implement", a fake return value, or
  mock data left in non-test code. Judge it at Stop over the turn's edits.
- **Stuck detection.** Judge the last N tool calls and their results: is
  Claude repeating an approach that keeps failing? A band offers "Step back
  and rethink." It uses the judge tool on PostToolUse after a failure.
- ⚙ **Commit messages.** On `git commit`, judge the message against the
  repo's convention. It is `commit-messages`, already on the roadmap.

### 4. Testing and QA

- ★ **Failure triage.** After `go test`, `npm test` or `pytest`, sort each
  failure into flaky, regression or environment. A band offers "Rerun the 2
  flaky ones". It is the `flaky-test` template from the skill, promoted to a
  built-in.
- **Assertion strength.** In a new test, does each assertion check something
  meaningful? This catches `assert.NotNil(result)` as the only assertion.
- **Generated-test pruning.** When Claude writes 30 tests, rank them by
  value and flag redundant ones with the judge tool.

### 5. Security and vulnerabilities

- ★ **MCP tool poisoning at SessionStart.** Run `prompt-injection` on every
  MCP tool's name and description. It runs once per session, so the latency
  is free, and it reuses an existing template.
- ★ **Instruction-file injection.** Run `prompt-injection` on CLAUDE.md,
  AGENTS.md, `.cursorrules` and skills from third-party repos, at
  SessionStart and on Read. Rules files are a known attack vector. Like the
  content check, this check can't be turned off, because an attacker
  controls its input.
- ⚙ **Secrets.** Entropy and pattern hits on writes and command output come
  first. Decide then answers "a real credential or a fixture?" This is the
  roadmap's `secrets` template; the CWE pack's hardcoded-credentials check
  is the same template, not a second one.
- **Untrusted Reads.** Run `prompt-injection` when Claude reads files in
  `node_modules`, vendored code or `~/git/lib`, not only on web content.
- **Vulnerability templates on diffs.** The CWE pack (see [Bug and
  vulnerability hunting](#bug-and-vulnerability-hunting)), run with `--each
  function` on Claude's own edits.
- ⚙ **Vulnerability triage.** Use `govulncheck` and `npm audit` output, or
  Dependabot alerts, as items. Rank them by how likely each is to be
  exploitable in this codebase's usage.
- **Exfiltration.** Extend `command-risk` with an `egress` question: does
  the command send file contents to a host outside the project?

### 6. Code review

- ★ **Review-comment triage.** Before Claude addresses a review, judge each
  comment: blocking, nit or question; already addressed; fix, decline or
  defer. This pairs with the `address-pr-review` skill and shows the judge
  tool on volume.
- **Every thread answered.** At Stop, after a turn that addressed a review,
  check whether each thread got a reply or a fix.

### 7. Release and deploy

- ⚙ **Changelog gap.** It is free to tell that CHANGELOG is untouched.
  Decide asks only whether the diff is visible to users.
- **Docs drift.** For each section of the README and docs, does the diff
  contradict it? This repo's own AGENTS.md asks for exactly this.
- ⚙ **Migration safety.** Run on files in `migrations/`: does the change
  take a long lock, rewrite a table, or drop data irreversibly?
- ⚙ **Deploy and prod gating.** Parse the target from `gcloud run deploy`,
  `wrangler deploy`, `terraform apply`, `kubectl` or a `psql` connection
  string. Decide judges whether an ambiguous host is production, and if so
  you choose whether it runs.
- **Release-note ranking.** Claude writes the notes; decide orders the
  changes by their impact on users.

### 8. Operations and incidents (SRE)

- ★ **Log triage at volume.** Run over `cwlogs` shards with the CLI, or the
  judge tool for a slice: error class, novel or known, and severity. It
  turns 10,000 lines into the 40 worth reading, and it is the volume story
  that Claude alone can't match on cost.
- **Alert dedupe and routing.** Run `ticket-routing`-style templates over
  alerts.
- **Postmortem lint.** Does the postmortem name a root cause, an owner and a
  follow-up that prevents a repeat?

### 9. Maintenance and autonomy

- ★ **Nightly backlog routine.** A scheduled routine runs `task-readiness`
  with size over open issues and hands the top three to `/orchestrate`.
  "Claude works while you sleep", built from templates that already exist.
- ★ **Worktree fan-in.** When parallel agents finish, use the judge tool to
  ask the same questions of each diff: does it meet the request, does it
  tamper with tests, what is its risk? Rank which branch to merge.
- **Dependency upgrades.** Claude reads the upstream changelog. Decide
  judges each listed change: "does it affect an API we call?"

### Other functions

- **Compliance and privacy.** PII in new log lines, PII-bearing fields in
  new schemas, and the license compatibility of new dependencies (license
  lookup first).
- **Accessibility.** a11y risks in JSX edits, such as a missing label or a
  click handler on a `div`.
- **Cost.** Infrastructure changes that raise cost: `min-instances`, machine
  sizes, unbounded autoscaling.
- **Agent supervision:**
  - **PreCompact constraint check.** After compaction, did the summary drop
    a constraint the user set, such as "don't touch X"? A band lists any it
    dropped.
  - **Status-line trust meter.** A rolling score per session from flags,
    overrides and unverified claims, which tells the user when to read more
    closely.

## Bug and vulnerability hunting

> **Direction, 2026-10-04:** build universal checks first, then language,
> project and bug-specific checks, in that order. That reverses this
> section's lead. See [check-packs.md](check-packs.md), which maps the
> template packs to CWE and OWASP.

Every idea above judges what Claude is doing right now. This section turns
decide on the code that already exists. The shape is a funnel:

1. **Cheap:** decide's own source scanners find the candidates, with no
   model call (see [Source
   analysis](#source-analysis-use-whats-already-there)).
2. **Volume:** decide judges every candidate.
3. **Expensive:** Claude reads the top N, confirms each, writes a failing
   test and fixes it.

Decide narrows the field and Claude decides. Neither claims a bug alone.
Only what reaches a failing test is reported as a bug. The rest are
"suspected", with decide's answers as evidence. A sweep that reports 300
maybes gets ignored.

### A. Your project's history: variant analysis (lead with this)

A bug that happened once usually exists in more than one place. Security
teams call the search for the others variant analysis. CodeQL does it across
repos, but only for people who write QL. Decide can do it from one example,
in plain language, and the evidence is already in the project's GitHub
history. Of everything in this section, this is the most distinctive idea.

- ★ **Fix one, find all.** When Claude fixes a bug (a `fix:` commit, or a PR
  that closes an issue), a band offers "Look for the same bug elsewhere?"
  Claude writes the pattern as one `noul` question, such as "Does this
  function read a map that another goroutine writes, without the lock, as in
  #87?" The template's `match` on the APIs involved narrows the functions.
  Decide judges each one, and Claude fixes the confirmed ones in the same
  PR.
- ★ **Bug fingerprints.** Each fixed bug leaves one question in
  `.decide/templates/known-bugs/`, linked to its issue. It runs on each diff
  in the plugin, in `/decide ship` and in CI: "This edit reintroduces the
  pattern of #87." The project's regressions become its checks.
- ★ **Graduate fingerprints to rules.** Sometimes a fingerprint's `match`
  alone turns out to find the bug: every match is a bug, and decide's
  answers add nothing. Then the template becomes rule-only, so the match
  flags the item with no model call, and the recurring cost goes to zero.
  Decide covers the period while a pattern is new or too semantic for a
  rule. A team that already runs Semgrep can export the rule there too.
- **Labeled examples from history.** The buggy function before its fix
  commit is a "yes", and the same function after the fix is a likely "no".
  This gives `decide eval` real examples from this codebase. There are three
  catches:
  1. A fix commit touches several functions, so Claude must pick the one
     that held the bug.
  2. "After" isn't proven clean.
  3. Fixes in public repos may be in the models' training data, which
     inflates recall.

Prefer fixes made after the model's cutoff, and say so when a public example
is used.
- **Mining past fixes.** A one-time backfill. Run `gh pr list --state merged
  --search fix` and `git log --grep fix`. The judge tool keeps fixes whose
  pattern could recur, such as a missing nil check, and drops one-offs, such
  as a typo in a string. Claude writes a fingerprint for each one it keeps.
- **Review-comment mining.** Past review comments hold the team's unwritten
  rules ("we always set a timeout here"). The judge tool clusters them and
  keeps the ones that recur. Each becomes a question for `/decide ship`: the
  reviewer you'd get, before you get them.
- **Postmortems and advisories.** Incident write-ups and GitHub Security
  Advisories for the project's dependencies become fingerprints the same
  way.

### B. Deviance: the code's own conventions

Engler's insight: code tells you its rules by how often it follows them. If
18 of 20 handlers call `requireAdmin` and 2 don't, those 2 are the
candidates.

- ★ **A deviance check is a template.** `match` selects the peer group, such
  as the handlers under `admin/`, or the functions that call `Open` or
  `Lock`. The question asks for the convention and allows for a reason:
  "Does this check admin access, or have a clear reason not to?" decide
  judges every peer, and the outliers are the candidates. Claude writes the
  template once, after noticing the convention while reading the code. It is
  committed and reruns in CI with no code of its own. Bugs where something
  is missing, such as authz, a `Close` or a `ctx`, have nothing to search
  for, so this is the way to find them.
- **Judge outliers among near-duplicates.** In 20 copy-pasted handlers, the
  bug is usually in the one that differs. Diff each copy against the
  cluster's common form and judge the differences.

### C. Common patterns: template packs

- **CWE pack.** One template per common vulnerability class: SQL injection,
  path traversal, SSRF, missing authz, XSS, open redirect, unsafe
  deserialization, weak crypto, and hardcoded credentials (the `secrets`
  template).
- **Language packs.** Bugs reviewers keep catching:
  - **Go:** an unclosed `resp.Body`, a missing `ctx`, a goroutine with no
    way to stop, `defer` in a loop, a shadowed `err`, `io.ReadAll` with no
    limit, an `http.Client` with no timeout, and a map written from several
    goroutines.
  - **TypeScript:** a missing `await` and an unhandled promise.
  - **SQL:** queries built by concatenation.

Most of these have syntactic candidates, so `match` finds them, and decide
judges whether each is a real problem in context.
- **Ranking another scanner's findings (optional).** decide doesn't depend
  on a scanner. But SARIF is a file format, so if a team already runs
  Semgrep, CodeQL or gosec, reading their findings is cheap interop. Each
  finding becomes an item, with its code fetched from the file. Rank them by
  "a real issue here?" and show how many ranked low; never drop them
  silently. Semgrep Assistant and Copilot Autofix already sell this, so it
  is a convenience, not the lead.

### D. Issues as knowledge

- **Similar past issues.** When Claude picks up a bug report, the judge tool
  ranks closed issues by similarity. Claude reads how the closest one was
  fixed before it starts.
- **Duplicate detection.** Does a new issue duplicate an open one? A
  scheduled routine labels likely duplicates.
- **Bug localization.** Rank the functions most likely to hold a reported
  bug, after a pre-filter by stack trace and keywords. Claude starts at the
  top three instead of grepping blindly.

## Checks at scale on large existing codebases

The goal is comprehensive in the end, efficient at every step. 2 million
lines of Go or Java hold roughly 80,000 to 130,000 functions. Asking every
question of every function is too slow, too costly and too noisy.

### 0. The cost model

- **Price it against Claude Haiku.** 100k functions × about 1,500 tokens is
  about 150M input tokens per pass. On Haiku that is on the order of $150.
  The volume argument holds only if decide is clearly cheaper, or better
  calibrated, at that scale. Measure it and put the number in the README.
  Without it, the scale story is a claim. It depends on the roadmap's token
  usage item.
- **Questions are independent, so ask many at once.** Jev evaluates "every
  question ... in parallel and in isolation against the same state", so
  "adding more questions does not create context-rot"
  ([introduction](https://docs.typesafe.ai/introduction)). TypeSafe's
  [parallel
  questions](https://docs.typesafe.ai/cookbooks/parallel_questions.md)
  cookbook measured it on a document of about 54,000 characters:
  - 13 questions in one request vs 13 single-question requests, each run 5
    times;
  - batching was 12.2× cheaper and 10× faster, "with no change in answers".

So the cost is per item, not per question. Group every question an item
needs into one request, and cache each answer under its own key. The CLI
already does the grouping: `Template.Decode` puts all of a template's
questions in one request, and the judge tool runs through the CLI. The judge
tool's limit of 8 questions is the plugin's own, not the API's. Still to
confirm: whether Clef answers questions independently too.

### 1. Where the work lives

The CLI gains a few generic primitives: an answer cache, a prefilter in the
template, call context from its own scanners, a cost preview, a limit for
hot code first, SARIF output, a run diff and eval. It installs and requires
nothing else. Claude and the plugin orchestrate by calling it. Anything
specific to one codebase is a template, which is reviewed, committed and
rerun in CI, not a custom program. The root package doesn't change. See [Who
does what](#who-does-what-the-cli-the-plugin-and-claude), below.

### Source analysis: use what's already there

decide adds no analyzers and requires none. Two sources cover it:

- **decide's own scanners, for every run.** `internal/source` already finds
  functions in Go, Python, JavaScript, TypeScript and Java: Go through
  `go/parser`, and the others through chroma's tokenizer. With them, `match`
  can name calls and imports and skip comments and strings, and `--context
  calls` can add the functions an item calls. Nothing new to install.
- **The project's own toolchain, for anything deeper.** Wherever a project
  builds and runs its tests, its tools are already installed: `go` for a Go
  service, the local Python for a Python app, and `node` and `tsc` for a
  TypeScript one. Claude Code's LSP tool also works, where the project has
  it set up. Claude uses these the way it would anyway: to find callers,
  follow an interface, or check coverage. A failing test in the project's
  own test runner confirms a bug. decide doesn't wrap or require any of
  them.

When a tool isn't there, the step is skipped and the report says so.

### 2. Choose the unit well

- **Functions with a context slice.** An item is a function plus what it
  needs to be judged alone: the types it takes, and what it calls. Plain
  signatures make the model trust names, so a `sanitize()` that doesn't
  sanitize would pass. Instead, use **bottom-up summaries.** Ask each
  function once, "does it pass an argument to a sink?" and "does it sanitize
  its input?", cache the answers, and put them in its callers' slices in
  place of bare signatures.
- **A token diet.** Remove license headers and import blocks, and shorten
  long literals and embedded data. Keep comments, which carry intent.
- **Leave out what isn't code.** Generated and vendored code, lockfiles,
  fixtures and migrations already applied are left out, or swept with
  templates made for them.
- **Dedupe exact copies only.** Judge an exact copy once. Treat near-copies
  as deviance (B, above): judge the outliers, not one representative.

### 3. Route questions without losing recall

- **Route with `match`, wrappers included.** Each template's `match` names
  the calls and imports it cares about: SQL, `os/exec`, the file system,
  network, deserialization and templates. Indirect sinks are the recall
  risk: in-house wrappers such as `store.Exec`. Claude finds them once with
  the project's own tools and adds them to `match`, and `--context calls`
  shows the wrapper's body beside its caller. Functions that dispatch
  through interfaces or reflection get the broad pack, and the exploration
  lane covers what `match` misses.
- **Route missing-X bugs by structure.** Missing authz, a missing `Close` or
  a missing `ctx` have no sink to tag. Route them by entry point, by
  acquiring a resource, or by deviance.
- **An exploration lane.** Always spend a fixed share, say 5%, on items
  chosen at random from those routing left out. It is how routing's blind
  spots show up.
- **Cascades only where they cut deeply.** "Does this function touch
  security?" passes almost everything. Each stage's recall multiplies (0.95
  × 0.9 × 0.9 ≈ 0.77), and a funnel's drops are permanent. Use a cheap first
  stage only where it removes most items and its recall is measured. Log
  every drop with the funnel `Report`.

### 4. Prioritize

- **Attack surface first.** Enumerate the entry points: routes, CLI flags,
  queue consumers and webhooks. Walk the call graph from them.
  Injection-class questions go to what untrusted input reaches.
- **Hotspots.** Rank by churn, number of authors, past fixes and complexity.
  Bugs cluster where code changes often and was fixed before.
- **Order is the budget.** `--limit N` judges the hottest code first, and
  JSONL items Claude writes can list the riskiest first. Runs already
  resume, so the next night continues. Decide needs no `--budget` flag and
  no price table that goes stale.
- **A coverage matrix.** Packages × question areas, with the share judged.
  Failed items count as uncovered, not as judged. "Comprehensive" becomes a
  number you can watch rise.

### 5. Be incremental: diff-time is the steady state

The full sweep is a one-time backfill. After it:

- **Diffs are the main path.** The plugin, `/decide ship` and CI judge each
  change as it lands, with the fingerprints and packs.
- **Cache each answer.** The key is the item's hash plus the question's text
  and the model version. Because questions are independent (step 0), an
  answer can be reused whatever else was asked beside it. See [The answer
  cache](#the-answer-cache) for how far it goes, and where it stops.
- **Invalidate along reverse call edges.** When a function's body changes,
  its summary changes, and so do its callers' slices. Those callers are
  judged again. Otherwise a broken sanitizer never re-flags its callers.
  With `--context calls`, this comes for free: a callee's definition is part
  of its caller's item, so a changed callee changes the cache key. The
  nightly sweep covers only what was invalidated.
- **Fingerprints that survive edits.** A finding is identified by its
  qualified function name, the question, and a fuzzy hash of the context,
  sent as SARIF `partialFingerprints`. A hash of the function body alone
  would reopen the finding at every edit.
- **One home for dismissals.** Upload SARIF to GitHub code scanning and let
  it own dismissals. Don't keep a second baseline file. On private repos
  this needs GitHub Advanced Security, and an upload is limited to about
  25,000 results, so upload only ranked findings.

### The answer cache

The cache doesn't make the first pass cheaper. It makes every pass after it
cheap. Without a cache, cost grows with the codebase times the number of
runs. With one, you pay for the codebase once, then only for what changes.

**How it works.** Before each request, decide looks up:

```
key = hash(item text, with any --context) + hash(question) + model version
```

A hit is used with no request. A miss is asked and stored.

- **The model version.** The API returns the version it resolved, such as
  `jev-1.13.0`, in `Response.Model`. Entries are stored under that, so an
  upgrade behind `jev-latest` never serves stale answers.
- **Per question.** Questions are independent, so an answer stays valid
  whatever was asked beside it.
- **Safe to cache.** Jev is close to deterministic. TypeSafe's cookbook
  found most answers identical across 5 repeats, so a cached answer is what
  the model would almost certainly give again.

**Where it pays.** These examples assume 100k functions, one template and $1
per 1,000 requests.

| Case | Without a cache | With one |
| --- | --- | --- |
| Nightly sweep for 30 nights, with 0.5% of functions changing a day | 3,000,000 requests, $3,000 | 100,000 on night 1, then about 500 a night: about 115,000, $115 (26× less) |
| One PR judged by the plugin, `/decide ship`, CI on 5 pushes and a rerun | Every unchanged function, every time | Each unchanged function once, if the caches are shared |
| Replaying 50 commits of history (time-travel demo, recall measurement) | 50 full passes | One pass plus 49 small diffs |
| Generated code, copy-pasted handlers, a library vendored in several services | Every copy | Each identical body once |
| Rerunning a sweep that stopped | `runs resume` covers one run | Any rerun is free for what's done |

**Where it doesn't help:**

- **The first pass.** 100,000 functions are still 100,000 requests. `match`
  and `--limit` address that.
- **Adding a question.** Cost is per item, so a request for one new question
  pays for the item's text again. A new question over the same items costs
  about one more pass; the cache only spares re-asking the old ones. A
  narrow `match` keeps it cheap: a fingerprint that applies to 300
  functions, not 100,000.
- **Changing a question's wording.** Every answer to that question goes
  stale. Tune templates on a labeled sample with `decide eval`, then sweep
  once.
- **A model upgrade.** Everything goes stale at once, which is correct.
  Re-judge a sample first, and use `runs diff` to see what would flip before
  paying for the whole repo.
- **Context widens churn.** With `--context calls`, editing a function
  changes every caller's item too. Callers should be judged again, but churn
  costs a few times the bare edit.
- **Reformatting.** `gofmt` or Prettier over the whole repo changes every
  hash. Ignoring whitespace in the key would help, though it can matter in
  Python.
- **Claude's live edits.** A hunk Claude just wrote is new text, so the
  plugin's in-the-loop checks rarely hit. The cache pays off for sweeps, CI
  and history.
- **Separate machines.** A laptop and CI each keep their own cache unless CI
  saves it with `actions/cache`. A cache shared across an org would need a
  remote store, which comes later.

**The big picture.** Three levers together make a large codebase affordable:

- `match` decides how broad: only the code a question applies to.
- `--limit` decides how much a run does: the riskiest first.
- The cache decides how often anything is paid for: once.

The cache turns "sweep the codebase" from a costly one-off audit into
something that runs every night and on every PR. Only then do "fix one, find
all" and fingerprints on every diff become everyday tools.

### 6. Measure recall honestly

- **Canaries through the whole pipeline.** Copy real functions and plant
  known bugs in them. Run them through the pre-filter, tags, routing and
  cascade, not only the question. A planted "drop the authz call" removes
  the very tag that would route it, and only an end-to-end canary shows
  that. Planted bugs are easier to spot than real ones, so canary recall is
  an upper bound.
- **History, post-cutoff.** The labeled examples from variant analysis (A),
  preferring fixes newer than the model's training data.
- **Sampled audits, stratified, with honest statistics.** Claude reviews a
  random sample of items judged clean, stratified by tag and score band. At
  realistic prevalence the numbers are small. With 0 misses in n samples,
  the miss rate is below 3/n at 95% confidence (the rule of three), so 150
  clean samples are needed just to say "under 2%". And Claude isn't ground
  truth, so this measures disagreement with Claude, not recall. Report it as
  that.
- **Fit thresholds for each repo** with `patterns/calibrate`, on labels from
  overrides, history and audits. Keep them in the plugin's or the CI job's
  config, never in the root package.
- **Disagreement, used carefully.** Jev and Clef aren't on one scale, and
  their errors correlate, so their agreement isn't correctness. Run a second
  model only on the uncertain band, and set that band from calibration, not
  from a fixed 40% to 60%.

### 7. Beyond single functions

- ★ **Extracted tables as items.** Deterministic extraction turns structure
  into items. The route table (route, method, middleware and handler)
  becomes one item per route: "is this route missing authorization for what
  it does?" The same works for SQL queries, config reads, outbound URLs and
  permission checks.
- **Call chains instead of taint paths.** An item is an entry point plus the
  functions it calls (`--context calls`). Ask: "does input from this handler
  reach a query without being escaped?" Claude confirms the likely ones with
  the project's own tools and tests.

### 8. Throughput and Claude's stage

- **Shard by package** across CI jobs or cloud agents, respect provider rate
  limits, and run at night as a scheduled routine.
- **Cluster before fixing.** Group confirmed candidates by pattern and
  package. One subagent in one worktree fixes 12 copies of the same bug in
  one PR, not 12 PRs.
- **Across the org.** A fingerprint from one repo is swept across every repo
  from `gh repo list`, with the same cache and routing. This is the scale
  story for platform and security teams.

### Who does what: the CLI, the plugin and Claude

There is no separate orchestrator. The CLI gains a few generic primitives,
and Claude and the plugin orchestrate by calling it. A capability belongs in
the CLI when all of these are true:

- **Deterministic.** The same input gives the same items and the same skips.
- **Needed without Claude.** It must work the same in CI, pre-commit and a
  plain shell.
- **Generic.** It works for any template in any repo, with nothing specific
  to one codebase.
- **Invisible or one flag.** A newcomer gets it by default, or by one plain
  flag.

Anything that writes text or depends on one codebase goes to Claude: the
questions, the `match` patterns and the confirmations. What Claude writes is
a template or a list of items, not a program to maintain. The plugin
supplies triggers, background runs, progress and the cost prompt.

#### The CLI: primitives that make a sweep cheap

1. ★ **An answer cache, on by default.** The key is the item's text, the
   question and the model version. Questions are independent (step 0), so
   each answer is cached on its own. Exact duplicates are judged once for
   free. See [The answer cache](#the-answer-cache). Rerunning a sweep pays
   only for functions that changed, so a nightly sweep is just the same
   command run again. The cache lives under `DECIDE_HOME`, and CI keeps it
   with `actions/cache`. `--no-cache` turns it off. This takes the roadmap's
   "request cache" further, and it is the biggest single saving.
2. ★ **A prefilter in the template.** `"match"` in `template.json` says what
   an item must contain to be asked about at all. It names calls and
   imports, read by decide's own scanners, so a comment or a string doesn't
   count. A regular expression remains for anything else. For
   `sql-injection`, it might be `{"calls": ["Query", "Exec"], "imports":
   ["database/sql"]}`, plus any in-house wrappers. Skipped items are counted
   in the summary ("4,012 skipped, no match"), never hidden. `--match`
   overrides it for one run. This is a deterministic prefilter in the CLI's
   template format, not routing policy in the root package. A fingerprint
   carries its own prefilter, so `known-bugs` stays cheap as it grows. It
   changes the template format, so it needs a proposal first.
3. **A cost preview.** `--dry-run` already shows what would be asked. Add
   the counts: items, skipped by `match`, already cached, and estimated
   tokens. The plugin shows these before a large run starts. It depends on
   the roadmap's token usage item.
4. **Hot code first.** `--limit N`, with items ordered by how often git has
   changed their file. The run summary says how many it left out, and `runs
   resume` continues where it stopped. This gives a budget and a priority
   without a price table.
5. **SARIF out, and optionally in.** `--format sarif` with
   `partialFingerprints` sends findings to GitHub code scanning. Reading
   SARIF is optional interop, for teams that already run a scanner.
6. **Context from what a function calls.** `--each function --context calls`
   adds the definitions of functions in the same run that the item calls,
   matched by name. It works in every supported language and needs no
   external tool. Measure it with `decide eval` before it ships.
7. **`decide runs diff A B`.** What flipped between two runs. Use it after a
   template or model change (drift), and before and after a fix. The
   time-travel demo uses it too.
8. **`decide eval`** (on the roadmap). It reads labeled JSONL. Claude writes
   the labels from history, canaries and overrides.

The CLI's knowledge of code stays within its own scanners: functions, names,
calls and imports. Anything deeper comes from the project's own toolchain,
run by Claude.

#### Claude: the parts that need judgment or writing

- Choosing what to sweep, with which templates and in what order.
- Writing fingerprint questions and their `match` patterns from a fix.
- Confirming with the project's own tools: callers, implementations, and
  whether untrusted input really reaches a function.
- Writing items directly when they are few: a route table of a few hundred
  routes is something Claude reads from the router and writes as JSONL, with
  no program.
- Planting canaries, and labeling examples from history.
- Confirming the top N with a failing test, then fixing them in clusters.

#### The plugin: triggers, background runs and the cost prompt

- **`/decide sweep`.** Claude plans the sweep and runs `--dry-run`. The
  plugin asks you to approve the item count and estimated cost, then runs
  the sweep in the background with progress in the status line. Claude reads
  the `--json` results and sends the top N to subagents to confirm.
- **"Fix one, find all".** After a fix commit, a band offers a search.
  Claude writes `.decide/templates/known-bugs/issue-87/template.json` with
  its `match`, then runs:

  ```sh
  decide run .decide/templates/known-bugs/issue-87 . --each function --dry-run
  # 41,000 functions · 312 match · 0 cached
  decide run .decide/templates/known-bugs/issue-87 . --each function --json
  ```
- **On scanner output, if a team has one.** When Claude runs a scanner the
  team already uses, the plugin ranks its SARIF with `decide run sast-rank`.
  Nothing requires one.
- **The same commands in CI.** Every command the plugin runs is a plain
  `decide run`, so a CI job or a scheduled routine can run it unchanged.

### The demo: time travel

1. Check out a public repo just before the first fix in a known cluster of
   related CVEs.
2. Give decide only that fix.
3. Show that it surfaces the siblings that were reported later.

A candidate is Gogs' 2024 cluster of argument-injection CVEs; Grafana's
plugin path traversal (CVE-2021-43798) is a backup. Verify both before using
them, and pick ones newer than the model's cutoff where possible. Pair the
demo with an end-to-end canary run, so recall is reported honestly alongside
it, and state the training-data caveat. Keep `demo/` for the plugin; it's
too small for a scale story.

## Cut or deferred, and why

| Idea | Why |
| --- | --- |
| Auto-allow safe commands | A confident false negative runs unprompted. Replaced by suggested allow-rules (P4). |
| Scope-creep check | A legitimate refactor touches distant helpers, so it would raise false positives on every real task. |
| Test adequacy | A coverage question; `go test -cover` answers it exactly. |
| Architecture fit, breaking changes, "does this package mix concerns?" | `go list` and `apidiff` know; a model guesses. |
| Deploy-to-error correlation | Needs the code and the incident in one item. |
| Reviewer routing | CODEOWNERS. |
| Verification gap at Stop | The removed reply check asked this and flagged one honest reply in five (#53). |
| Subagent reply check at SubagentStop | The same question as the removed reply check, which was not reliable enough to act on (#53). |
| Request clarity and convention match | Claude can do these itself; no gain from a second opinion. |
| Threat models, QA plans, release notes | Generation, not judgment. |
| `applies_to` routing by tags | Replaced by a regex `match` in the template. |
| Depending on Semgrep, CodeQL, ast-grep or a language server | Use decide's own scanners and the project's own toolchain. Reading SARIF stays optional interop. |
| Exact analysis inside decide (call graphs, `go/types`) | The project's toolchain already does it, and Claude runs it when confirming. Reopened by the [check-packs spike](check-packs.md#spike-results): call-path items cut false positives. |
| Scripts Claude writes for each repo | Unreviewed code that breaks as the repo changes. Use templates, `match` on decide's scanners, or items Claude writes directly. |
| `--budget $20` | Replaced by `--limit N`, hot code first; no price table to go stale. |
| A separate sweep orchestrator | The CLI's primitives, called by Claude and the plugin. |
| A committed baseline file | GitHub code scanning owns dismissals. |
| SARIF triage as the lead | A crowded market. Variant analysis is the distinctive story. |

## Risks to design for

- **Latency.** A refactor can be 40 edits. Limit each check by path glob,
  prefer PostToolUse warnings to PreToolUse blocks, run checks in parallel,
  consider `clef-flash`, and keep the one-minute fail-open cool-off for
  every check.
- **False-positive fatigue.** This is what kills the plugin. Every new check
  ships with `warn` as its default and with labeled examples. It escalates
  to `ask` only after `decide eval` clears a bar (P3).
- **Recall that only looks comprehensive.** Routing, cascades and dedupe
  each drop items. Measure end to end with canaries, keep an exploration
  lane, and count failures as uncovered.
- **Attacker-controlled inputs.** Any check that reads them can't be turned
  off by config: content, MCP descriptions, instruction files and vendored
  Reads.

## Suggested sequence (one PR each)

1. `test-tampering` template, planted demo cases, and the PostToolUse check
   on test paths. **Launch video.**
2. MCP and instruction-file injection scan at SessionStart.
3. Override capture into labeled examples, so `decide eval` has data.
4. `error-swallowing` and a built-in `test-failure` triage band.
5. P1 checks as config, with the existing checks moved into the default
   config.
6. P2 rulebook compile to a template, enforced through P1.
7. `/decide ship`.
8. ExitPlanMode review and the PreCompact constraint check.
9. Nightly backlog routine and worktree fan-in. This is the autonomy story,
   once the checks earn trust.

The hunting and scale track runs beside it. It is superseded by the order
in [check-packs.md](check-packs.md): packs first, then `where`; steps 2
and 3 below have shipped.

1. Measure first: the price against Haiku at 100k functions, and whether
   Clef answers questions independently like Jev.
2. The answer cache, on by default; then `match` in templates and the counts
   in `--dry-run`. They make step 3 cheap on a large repo.
3. "Fix one, find all" with the time-travel demo. **The scale launch.**
4. Fingerprints in `known-bugs`, run on diffs by the plugin, `/decide ship`
   and CI.
5. `match` on calls and imports, and `--context calls`. Then `--format
   sarif` with `partialFingerprints`, and `--limit`.
6. `/decide sweep` in the plugin: plan, preview, approve, run in the
   background, confirm.
7. Deviance templates for missing authz, `Close` and `ctx`, built on
   `match`.
8. `decide runs diff`, and end-to-end canaries for `decide eval`.
9. Rule-only fingerprints, and org-wide sweeps.
