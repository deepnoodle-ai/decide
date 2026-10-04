# Fix one, find all

Status: Phase 0 done: go. Phase 1 in progress. Updated: 2026-10-04.

> You fixed a bug. decide finds the other places it lives, and Claude fixes
> them in the same PR.

Security teams call this variant analysis. CodeQL does it for people who
write QL. decide can do it from one example, in plain words. See
[sdlc-brainstorm.md](sdlc-brainstorm.md#a-your-projects-history-variant-analysis-lead-with-this)
for where the idea came from.

## The workflow

1. **Trigger.** `/decide:hunt HEAD`, `/decide:hunt 87` for a PR. A band
   that offers the hunt after a `fix:` commit comes later.
2. **Fingerprint.** Claude reads the fix diff and the issue. It writes
   `.decide/templates/known-bugs/issue-87/template.json`: one or two `noul`
   questions, such as "Does this function read a map that another goroutine
   writes, without the lock, as in #87?", a prefilter on the calls and
   imports involved, and a README that links the issue.
3. **Self-test.** Claude runs the template on the function before the fix
   and after it. It must flag the first and pass the second. If not, Claude
   rewrites the question. This needs no new code: the items are JSONL.
4. **Preview.** `decide run … --each function --dry-run` shows the count:
   "41,000 functions · 312 match · 0 cached".
5. **Sweep.** The same command with `--json`. The answer cache makes a
   second hunt after edits close to free.
6. **Confirm.** Claude takes the top N by probability and confirms each in
   a subagent, by reading the code and, where it can, writing a failing
   test.
7. **Report and fix.** Only what reaches a failing test is a bug. The rest
   are "suspected", with decide's answers as evidence. Claude fixes the
   confirmed ones in the same PR.
8. **Keep it.** The fingerprint is committed, and later runs on each diff.

## Sequence

### Phase 0: spike, no code

The risky assumption: one question, written from one fix, ranks the real
siblings near the top. Test it with today's CLI. Narrow with `--include`
instead of a prefilter.

- Pick 3 to 5 real clusters: bugs fixed in one place, with siblings fixed
  later. Prefer our own repositories, with fixes after the model's training
  data. Add one or two public CVE clusters, such as Gogs' argument
  injection or Grafana's plugin path traversal (CVE-2021-43798).
- For each: check out the commit before the sibling fixes, write the
  fingerprint from the first fix only, and sweep.
- Measure the rank of each known sibling, the false positives above it, and
  the cost.
- **Go:** the siblings rank in the top 20 in most clusters. **No-go** tells
  us why: the question, the item size, or context across functions.

Results go in [Phase 0 results](#phase-0-results).

### Phase 1: plugin, `/decide:hunt`

Phase 0 found siblings with no prefilter, so the hunt comes first and needs
no CLI change. It is a plugin skill that takes Claude through steps 2 to 7,
with the self-test and the fan-out to confirm.

- **One question.** A second, "guarded on purpose", would double the cost
  and need a rule to combine two answers. The wording, the self-test and
  the confirm step cover it. Add one only if confirms show it is needed.
- **A flat layout:** `.decide/templates/bug-87/`, or `bug-<short sha>`
  with no issue. Template names have no `/`, and `decide templates` lists
  one level, so `known-bugs/issue-87/` would not show.
- **`matches` from the start,** so a kept fingerprint gates a diff with
  `--fail-on matched`.
- The report: confirmed, suspected and dismissed, with counts.
- No new hook. The command is explicit, so it is safe to demo.

First run, on dive's `bufio.Scanner` fix (`ed6f9be`) at today's code: the
self-test passed (0.55 before the fix, under 0.2 after), and
`monitorTool.Call` ranked 1st of 2,727 functions at 0.86, in two minutes.
The next two were dismissed on reading: one sets an 8 MB buffer, the other
has no Scanner.

### Phase 2: CLI, `where`

Add it when a hunt on a large repository needs it. The name can't be
`match`: templates have `matches`, the answers a user looks for.

- `"where": {"calls": ["Lock"], "imports": ["sync"], "text": "regex"}` in
  `template.json`. Any value in a field holds; every field must hold.
  `calls` and `imports` ignore comments and strings.
- Calls and imports from `internal/source`: `go/parser` for Go, chroma
  tokens (a name before `(`) for Python, JavaScript, TypeScript and Java.
- Skipped items are counted, never hidden: "40,688 skipped by where" in
  the summary and the dry run.
- It is generic: template packs and deviance checks use it too.
- It changes the template format, so it needs a proposal first.

### Phase 4: demo and launch

- The time-travel demo on the best cluster from Phase 0: check out the
  code before the first fix, give decide only that fix, and show the
  siblings reported later.
- Report recall over all the clusters, not only the best. Say that public
  fixes may be in the model's training data, and lead with our own
  clusters after the cutoff.
- A post: "Variant analysis in plain words: no QL, one example."

### Phase 5: follow-ons, one PR each

- A band after Claude commits a `fix:`: "Look for the same bug elsewhere?"
- Fingerprints on each diff, in the plugin, `/decide ship` and CI: "This
  edit brings back #87."
- Mining past fixes: a one-time backfill of fingerprints from merged fixes.
- `--context calls`, if Phase 0 shows bugs that span a caller and callee.

## Risks

- **Vague questions** that flag everything. The self-test on the fix, and
  ranking by probability instead of flags, guard against it.
- **Bugs across functions.** One function as an item misses them. Phase 0
  measures how often.
- **Training data** inflates recall on public CVEs. Lead with clusters
  after the cutoff, and say so.
- **In-house wrappers** escape a prefilter. Claude adds them to `where`,
  and the summary shows what was skipped.

## Phase 0 results

**Go.** In 6 of 7 clusters, a known sibling ranked in the top 20 of 1,300
to 2,900 functions, and 7 of 7 after rewording the wonton question. 11 of
15 siblings did, then 12 of 15. Every seed ranked in its
cluster's top 4, so the self-test (step 3) works.

Setup: `decide` from `main` (Jev, `jev-1.13.0`), `--each function` over all
non-test Go files at the seed's parent commit, one `noul` question written
from the seed fix only. No prefilter. 14,098 requests in all.

| Cluster | Functions | Known sibling | Rank | p | Above 0.5 |
| --- | --- | --- | --- | --- | --- |
| Gogs: git option injection (CVE-2026-26194 → CVE-2026-52806) | 2,029 | `PullRequest.Merge` | 3 | 0.75 | 8 |
| Gogs: path traversal (CVE-2026-24135 → -23633, -52813) | 2,013 | `UserPath` | 3 | 0.80 | 25 |
| | | `RepoPath` | 11 | 0.61 | |
| | | `SettingsGitHooksEdit`, `…Post` | 74, 67 | 0.18, 0.21 | |
| Gogs: symlink check (CVE-2025-64111 → CVE-2026-52811) | 2,012 | `UploadRepoFiles` | 3 | 0.86 | 6 |
| Gogs: repo ownership by ID (CVE-2026-25120 → -25229) | 2,015 | `UpdateLabel` | 1 (tie) | 0.91 | 103 |
| dive: retries of permanent errors (`39973d6`) | 1,318 | `openaicompletions` `Generate`, `Stream` | ≤6 | 0.94, 0.93 | 12 |
| dive: `bufio.Scanner` line limit (`ed6f9be`) | 1,840 | `monitorTool.Call`, `ReadFileTool.Call`, `GrepTool.parseRipgrepOutput`, `BashTool.execute` | 1, 2, 3, 6 | 0.75–0.88 | 9 |
| wonton: UTF-8 as bytes (`0d5b840`) | 2,871 | `PasswordInput.readMasked`, `updateMaskedDisplay` | 73, 51 | 0.29, 0.46 | 46 |

What we learned:

- **It works when the bug is visible in the function.** Five clusters put
  every sibling in the top 6.
- **Bugs across functions are missed.** The Gogs hook handlers pass a name
  to `GitRepo.Hook`, and the path is built inside the library. That is the
  case for `--context calls` (Phase 5).
- **The wording decides recall, not the item size.** The first wonton
  question listed hints such as "indexing a string" and "`len()` as a
  character count". It scored correct rune-based code such as `deleteChar`
  at 0.91, put 46 functions above 0.5, and ranked `readMasked` 73rd. Cutting
  the item into 20-line windows barely moved it (0.26 to 0.33). A narrow
  question about the seed's own mistake, a rune and byte conversion, ranked
  `readMasked` 2nd of 2,871, with 2 functions above 0.5. The next was 0.22.
  So Claude writes the question about the mistake in the fix, not its
  category, and the self-test also checks that nearby correct code passes.
  `updateMaskedDisplay` stays missed: its `len(buffer)` is wrong only
  because of the cast in `readMasked`.
- **Broad questions are noisy.** The ownership question put 103 functions
  above 0.5; the symlink question put 6. Ranking, not flags, is the
  output, and Claude confirms the top N.
- **It finds open bugs.** dive's `monitorTool.Call` still has the
  `Scanner` bug today, and ranked first.
- **The demo:** Gogs git option injection. One fix to `git tag -d` finds
  the CVSS 9.9 remote code execution in `PullRequest.Merge`, fixed four
  months later, at rank 3 of 2,029. Its fixes date from 2026, after the
  model's likely training data.
