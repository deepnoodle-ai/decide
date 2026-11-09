# decide eval: grade a template on labeled examples

Status: proposed, not built. Written 2026-10-20, revised 2026-11-09.
Issue #87.

> Write examples where you know the answer. decide answers them, and eval
> says how often it agreed with you, where it was wrong and sure, and how
> many cases each flag missed or raised by mistake.

```sh
decide eval command-risk examples.jsonl
decide eval command-risk                     # the template's own examples
decide eval command-risk --fail-under 0.9 --max-misses 0   # for CI
```

eval ships in two pull requests. The first grades: agreement, confident
mistakes, misses and false alarms for each flag, two CI gates, and
`--json`. The second suggests thresholds (section 6), once the examples
are numerous enough to fit one. Each later section says which PR it
belongs to when that is not the first.

eval keeps the `decide run` flags that still apply: `-p`, `--provider`,
`--model`, `--workers`, `--no-cache`, `--dry-run`, `--yes` and `--json`.
The examples file fixes what one item is, so `--each`, `--field`,
`--items`, `--include`, `--limit` and `--sample` go.

## 1. The examples file

JSONL, one example per line:

```jsonl
{"input": "rm -rf ~/", "labels": {"severe": true, "destructive": true}}
{"input": "ls -la src", "labels": {"severe": false}}
{"id": "outage", "input": "Nobody can log in.", "labels": {"urgent": true, "impact": 4}}
{"input": "Refund for order 1182", "labels": {"queue": "billing"}}
```

- `input` is what the model sees: a string, or an object where a
  template's items are objects.
- `labels` maps question keys to the right answer: `true` or `false` for
  noul, an option key for choice, and a level from 0 for score. That is
  the scale flags use (`"impact": ">= 3"`) and decide prints ("3.0 of 4").
- Labels may be partial. A question with no label is asked but not graded
  for that example, so a file can label only `severe`.
- `id` is optional and defaults to the item's label, `examples.jsonl:3`.

**Inputs must match what `decide run` sends.** eval grades the prompt it
sends, so an object input has to have the same fields a run would build,
not a hand-picked subset. The shape depends on how the run read its data:
`code-risk` over files sends `{path, language, function, lines, context,
content}`, and over a diff (`git diff | decide run code-risk --each
function`, as the demo runs it) sends `{path, old_path, language, change,
function, context, lines, part, diff}`. Otherwise eval grades a prompt
production never sends, and misses the cache. The reliable source is a
real run of the same kind: each saved run keeps the exact `state` it sent in
`DECIDE_HOME/runs/<id>/inputs.jsonl`. The docs show how to copy inputs
from there. A command that turns a run into an examples file is
follow-up work.

**Parameters.** eval takes `-p` as run does. The file does not record
parameters, so its labels are taken as written for the ones given. The
report header and `--json` print them, so a CI log shows what was graded.

**Reading.** eval reads the file with `internal/source`, as
`decide run t examples.jsonl --field input` would. `record` sends the
`input` field as `State` and keeps the whole record in `Item.Value`, so
labels come back with each result and need no join. Only whole items do
that: a record cut into parts keeps a 200 character preview in `Value`,
losing its labels, and a run combines parts by flag. So an example too
large to send whole is an error. Before any request, eval checks each
label against its question: an unknown key, a wrong type, an unknown
option or a level off the scale is an error. So are two examples with the
same input, compared as canonical JSON, which would count one case twice
in every number. Each error names the line.

**Where examples live.** In `examples.jsonl` beside the template's
`template.json`, wherever the template is found: built in
(`internal/template/builtin/<name>/`, embedded with `template.json` and
`README.md`), in the project's `.decide/templates/<name>/`, in
`DECIDE_HOME/templates/<name>/`, or in a directory named by path. With no
file named, eval uses the template's own. An offline test checks each
built-in file against its template, as `TestDemo` checks the demo.
`decide templates new --from` should copy `examples.jsonl` with the
template: labels describe the items, not the wording, so they are the
test suite a reworded copy needs. It copies no files today
(`template.Create` writes only `template.json`), and a built-in's file
comes from the embedded FS, so this is its own later PR (section 7).

Rejected: labels as top level keys, which collide with fields of object
inputs. A labels file joined by ID, which breaks when either file is
edited. CSV, which can't hold typed, partial labels or object inputs.

## 2. Grading each answer type

Agreement compares the answer decide finds most likely with the label. It
reads no flag and no threshold.

| Type | decide's answer | Right when |
| --- | --- | --- |
| noul | what decide prints: yes at 60% or more, no at 40% or less, unsure between | it equals the label; unsure never does |
| choice | `Choice`, the most likely option | it equals the label |
| score | `Level()`, the most likely level | it equals the label |

**Unsure counts as wrong.** It stays in the count, and the report shows
how many were unsure. Leaving unsure out would let a model that hedges on
the hard cases raise its own agreement, and grading at 0.5 would call
right an answer decide prints as unsure. A third bucket outside agreement
would need its own gate.

**Exact level for score.** One rule for choice and score, and the one
`calibrate` uses (`checkDataset` predicts the most likely level), so
agreement and the later fit agree on what a mistake is. Score also shows
agreement within one level, and the mean distance of `Score`, the
expected level, from the label, as information only. Rejected: rounding
`Score`, an expectation, so an answer split between levels 0 and 4
rounds to 2, which the model thinks unlikely. Rejected: grading within
one, which on a three level scale passes nearly everything.

**Agreement ignores flags** because the issue names two fixes for a bad
grade: reword the question, or move the threshold. Agreement measures the
question and the flag table measures the threshold. One number would not
say which fix to make.

**What agreement does not prove.** Agreement and confident mistakes say
where decide is wrong. They do not show that "83%" is right 83% of the
time; that needs a calibration measure over many examples per range. The
report and docs must not claim it.

## 3. The report

```
command-risk on examples.jsonl · 50 examples · typesafe jev-latest
50 asked: 41 from the cache, version unknown; 9 answered by jev-1.13.0

Agreement
  destructive   48 of 50   96%
  severe        47 of 50   94%   1 unsure
  leak          44 of 50   88%
  publish       no labels

Flags                         misses     false alarms
  severe   yes >= 80%         1 of 20    2 of 30
  leak     yes >= 80%         2 of 6     0 of 44
  publish  yes >= 80%         no labels

Confident mistakes: wrong at 80% or more
  examples.jsonl:7   severe   said no at 95%, labeled yes
                     rm -rf ~/
```

**Flags.** eval applies each flag's conditions to the label as a certain
answer: probability 1 for the labeled answer and 0 for the rest, or the
labeled level for a score. An example the flag would then mark is a
positive. A miss is a positive decide did not flag; a false alarm is a
negative it did. One rule covers noul, choice and score flags, and flags
with several conditions. A flag is graded on the examples that label
every question its conditions read; a flag with none is shown as "no
labels".

**Confident mistakes.** An answer wrong by section 2 whose chosen answer
had probability 0.8 or more. That is `gate.MeasureTop`, the number decide
prints. The 0.8 is fixed and follows no flag: flags differ by template
(`command-risk` flags at 80%, `security` at 50% and 90%, and a bare `yes`
at 60%, `DefaultFlagAt`), and the flag table already grades each
threshold. A fixed bar means "confident" reads the same for every
question. They are listed most confident first, with the input passed
through `clean`.

**Model versions.** A cached answer carries no version (section 6), so
the header names versions only for the answers asked in this run and
counts the rest as from the cache. The report never claims a version it
can't know.

## 4. `--fail-under` and `--max-misses`

`--fail-under 0.9` exits with code 2 when any graded question's agreement
is below 0.9, a fraction from 0 to 1. It compares the lowest agreement
across questions and names each question below the bar.

A question needs at least 10 labeled examples before the gate counts it:
below that, one example moves agreement by more than 10 points. With
`--fail-under`, a question with some labels but fewer than 10 is an error
before any request, naming the question and how many more it needs.
Skipping it would pass a gate that checked nothing, and exit 2 would read
as a model failure. Without the flag, the report marks it "too few
labels". A question with no labels is not graded and not gated.

**Agreement alone can pass a broken safety check.** With 99 safe
commands and one dangerous one, a model that always answers "safe" agrees
on 99% and passes `--fail-under 0.9`, having missed the only case that
matters. So `--max-misses N` exits 2 when any graded flag misses more
than N labeled positives. A safety template's CI runs both.

`--max-misses` follows the same rules as `--fail-under`. A flag with no
labels is not graded and not gated, so a file that labels only `severe`
can gate `severe`. A flag with some labels needs at least 10 graded
examples and one labeled positive; with `--max-misses`, fewer is an error
before any request, naming the flag and what it lacks, since a flag
graded on two examples would pass on one lucky answer. It is also an
error when no flag can be graded (the template has no flags, such as
`relevance`, or none is labeled). The report prints each gated flag's
positive count, so a thin gate shows in the CI log.

**Nothing to grade is an error.** Before sending any request, eval exits
1 when the file has no examples, or when no example has a valid label for
any of the template's questions, and says which. So a gate can never pass
a run that graded nothing.

**Failed examples.** Exit codes match `decide run`: 0 when all pass, 2
for a gate as for `--fail-on`, 130 when stopped, and 1 for an error or a
run where any example failed. An example with no answer can't be graded,
and counting it either way would move the number. So eval asks every
example, then prints the report over the answered ones, lists the failed
examples above it, and exits 1 without checking the gates, since the
counts are incomplete. It says to run the same command again: the cache
keeps the answered ones, so the rerun asks only the failed. An example
that fails every time, such as one the provider refuses, still leaves a
report to read, and removing it from the file lets the gates run.

Rejected: the mean across questions, which lets one broken question hide.
Rejected for `--fail-under`: grading flags, which would fail CI when only
a threshold moved. Flags have their own gate, `--max-misses`. Rejected
for now: a bar per question, a rate for `--max-misses`, and a template
saying which mistake costs more. A count bounds the mistake the issue
cares about; the rest can follow a real need.

## 5. Cache, cost and output

**Cost.** One request per example, with all the template's questions in
it: 50 examples is 50 requests. eval asks before running more than
`confirmAbove` (100), as run does, and `--dry-run` prints the count and
how many the cache answers.

**Cache.** eval creates a run with `runs.Create` and runs it with
`UseCache` and `Execute`, as `decide run` does. So the cache is on by
default and `--no-cache` asks fresh. A rerun after a threshold change
sends nothing, and rewording one question asks only that question again,
since its text is in the key.

**In CI, keep the cache.** The cache lives in `DECIDE_HOME/cache`, and a
fresh CI runner starts with none, so every push would pay for the whole
eval and could see answers drift between pushes. The CI docs show
restoring and saving `DECIDE_HOME/cache` between runs, for example with
`actions/cache` keyed on the examples file. With that, eval is cheap on
every push, with the frozen answers a gate wants.

**Drift.** Changing the provider or model name changes the cache key, so
those ask fresh. What a cached eval misses is drift behind unchanged
settings: the provider updating the model behind `jev-latest`. Only
`--no-cache` measures the model as it is today. The docs recommend cached
evals on each push and a `--no-cache` eval on a schedule or before a
release.

**No saved run.** eval discards its run with `Run.Discard` after the
report. `Discard`'s doc comment speaks of runs that were never ready; it
removes a finished one the same way, and the implementation updates that
comment to say so. The answers stay in the cache, so a rerun is cheap,
and nothing new enters the saved run format: no `"kind"`, no eval in
`decide runs list`, and no `runs resume` for a run whose gates it could
not keep.

**Formats.** Text, and `--json`, which prints one object, since the report
is a summary, not a line per item. It is printed unchanged, as run's is.

```json
{"template": "command-risk", "examples": "examples.jsonl", "params": {},
 "provider": "typesafe", "model": "jev-latest", "versions": ["jev-1.13.0"],
 "count": 50, "cached": 41, "failed": [],
 "questions": {
   "destructive": {"type": "noul", "graded": 50, "agree": 48,
   "unsure": 0, "agreement": 0.96, "confident_mistakes": 0},
   "severe": {"type": "noul", "graded": 50, "agree": 47,
   "unsure": 1, "agreement": 0.94, "confident_mistakes": 1},
   "leak": {"type": "noul", "graded": 50, "agree": 44,
   "unsure": 0, "agreement": 0.88, "confident_mistakes": 0}},
 "flags": {"severe": {"flag": "yes >= 80%", "positives": 20, "negatives": 30,
   "misses": 1, "false_alarms": 2},
   "leak": {"flag": "yes >= 80%", "positives": 6, "negatives": 44,
   "misses": 2, "false_alarms": 0}},
 "mistakes": [{"id": "examples.jsonl:7", "question": "severe", "label": true,
   "answer": "no", "probability": 0.95, "confident": true}],
 "fail_under": 0.9, "below": [], "max_misses": 0, "over": ["severe", "leak"]}
```

`unsure` counts answers already outside `agree`; only noul questions have
it. Score questions add `within_one` and `mean_distance`. `versions`
lists the versions of the answers asked in this run; `cached` counts the
rest. Questions and flags with no labels are left out of `questions` and
`flags`; one with too few to grade carries `"reason": "too few labels"`
and no counts.

## 6. Suggested thresholds (second PR)

Not in the first PR. A suggestion needs about 20 labeled positives and 20
negatives per flag, which the first examples file does not have
(section 7), and it brings most of the hard cases: the split, the fit,
and which answers it may trust. The first PR's report and JSON leave room
for it; the second adds a `suggested` line under each flag:

```
  severe   yes >= 80%         1 of 20    2 of 30
           suggested yes > 71%           held out: 0 of 10 missed, 3 of 15 false alarms
  leak     yes >= 80%         2 of 6     0 of 44
           no suggestion: 6 labeled yes, need 20
```

**The fit** reuses `calibrate.Fit`, for flags with one noul condition.
For `yes >= X`, eval calls Fit with `Measure: noul`,
`NoulTarget: "false"`, `MaxError: 0`, `MinAllowed: 1` and `ECEBins: 10`.
Fit then reads "allowed" as not flagged and "wrong" as labeled yes, so it
picks the highest cutoff that misses no labeled yes in the fit half. The
suggestion is `yes > cutoff`, rounded down to a whole percent so every
fit positive stays flagged. A `no` flag is the mirror, with
`NoulTarget: "true"`. Rounding flags more examples, so Fit's `Baseline`
describes another rule: 0.719 shown as `yes > 71%` can turn zero false
alarms into ten. eval takes only the cutoff from Fit, then grades the
rule it shows on each half, as it grades the template's flag. Bounding
misses first is the issue's own trade: "catch every dangerous command,
accept 3 false alarms." A miss lets the danger through; a false alarm
costs attention.

**Halves, balanced per flag.** For each flag, eval sorts its labeled
positives by a hash of the input as canonical JSON (keys sorted, no
spacing), and sends them to the fit and held out halves in turn; then
the same for the negatives. A plain hash parity would split positives at
random: with 20 positives, both halves get 10 only about 18% of the time,
so "need 20" would rarely be true. Alternating each class makes 20
positives exactly 10 per half. Adding an example can move others in the
sort, which is accepted: the halves are recomputed each run from the
whole file, and no state depends on them. The held out half never
changes the cutoff; it only reports it.

**When to show one.** Only when each half holds at least 10 positives
and 10 negatives for the flag, so 20 of each overall. Below that, one miss
moves the held out rate by more than 10 points, and `examples/calibrate`
already warns that six cases are too few. Otherwise the report says how
many more labels it needs. `ErrNoFit` means no cutoff lets one fit
example through without a labeled yes: the lowest scoring example is a
labeled yes or ties one, so any cutoff that leaves one example unflagged
misses a yes.

**Which answers it trusts.** Fit needs one resolved model version, such
as `jev-1.13.0`, and a cached answer carries none: `cache.Lookup`
returns no version and reuses answers whose version is unknown. eval
decides per flag, not per run: a flag gets a suggestion when its
question was answered fresh for every example (absent from each
`Result.Cached`) and every answer names the same version, which eval
passes to Fit. Rewording one question then asks it fresh and keeps its
suggestion, while other flags' cached answers withhold theirs. A
withheld suggestion says to rerun with `--no-cache`. Returning each
cached answer's version from `cache.Lookup` would lift this, and is
later work.

Rejected: fitting and reporting on the same examples, which grades the
fit on its own answers. Rejected: a new fitter in the CLI, when Fit is
built and tested.

## 7. Phasing

**First PR:** the examples file and its checks, agreement, confident
mistakes, the flag table, `--fail-under`, `--max-misses`, `--json`, the
CI cache docs, and `command-risk`'s examples. The seed comes from
`demo/commands.txt`: its README says which lines are severe (6, 7 and
9), so the seed labels `severe` on all 11 commands. The README says only
which lines are flagged by any flag, not by which, so it does not label
`destructive`, `leak` or `publish`. Eleven labeled examples clear the 10
`--fail-under` needs for `severe`, and three positives let
`--max-misses` grade it, so the CI command at the top runs on the seed.
They are far too few for a suggestion. The file grows with hard cases,
such as dangerous commands that look harmless, each label checked by a
second person.

**Second PR:** suggested thresholds (section 6).

**Later, each in its own PR:**

- Examples for each other built-in.
- Suggestions for choice and score. Fit takes only certainty measures for
  them, not the probability of one option.
- Writing a suggestion back into `template.json`.
- Misses and false alarms for matches, as `relevance` uses.
- Image templates, such as `receipt-quality`.
- `--format md`, `csv` and `github`. Markdown, for pull requests, is next.
- Comparing two template versions or models. `calibrate.Compare` does it
  from Go today.
- An eval API in the `templates` package.
- `decide templates new --from` copying `examples.jsonl` (section 1).
- A command that turns a saved run's inputs into an examples file.
- Examples too large to send whole, and Fit's `Artifact` in `--json`.
- Suggestions from cached answers, which need `cache.Lookup` to return
  each answer's version.

## Decisions

The first draft left these open. Each is settled above, where the
section gives the reason.

1. Score is graded on the exact level; within one is shown, not graded
   (section 2).
2. "Confident" is a fixed 0.8, not each flag's threshold (section 3).
3. A suggestion needs 10 positives and 10 negatives in each half, and
   the halves are balanced per flag (section 6).
4. `--max-misses` bounds misses for every graded flag, as a count.
   Rates and per-template costs wait for a need (section 4).
5. eval keeps no saved run, so nothing appears in `decide runs list`
   (section 5).
6. The command is `decide eval`. It is the word the issue, the roadmap
   and LLM tooling use, and Go callers of `decide.Eval` never see the
   CLI name.
7. `decide templates new --from` will copy `examples.jsonl`, in a later
   PR (section 1).
