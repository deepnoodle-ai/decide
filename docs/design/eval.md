# decide eval: grade a template on labeled examples

Status: proposed, not built. Written 2026-10-20. Issue #87.

> Write examples where you know the answer. decide answers them, and eval
> says how often it agreed with you, where it was wrong and sure, and
> where each flag's threshold should sit.

```sh
decide eval command-risk examples.jsonl
decide eval command-risk                     # the template's own examples
decide eval command-risk --fail-under 0.9 --max-misses 0   # for CI
```

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
  template's items are objects, such as `{"path", "function", "content"}`
  for `code-risk`.
- `labels` maps question keys to the right answer: `true` or `false` for
  noul, an option key for choice, and a level from 0 for score. That is
  the scale flags use (`"impact": ">= 3"`) and decide prints ("3.0 of 4").
- Labels may be partial. A question with no label is asked but not graded
  for that example, so a file can label only `severe`.
- `id` is optional and defaults to the item's label, `examples.jsonl:3`.

**Reading.** eval reads the file with `internal/source`, as
`decide run t examples.jsonl --field input` would. `record` sends the
`input` field as `State` and keeps the whole record in `Item.Value`, so
labels come back with each result and need no join. Only whole items do
that: a record cut into parts keeps a 200 character preview in `Value`,
losing its labels, and a run combines parts by flag. So an example too
large to send whole is an error. Before any request, eval checks each
label against its question: an unknown key, a wrong type, an unknown
option or a level off the scale is an error. So are two examples with the
same input, which would count one case twice in every number. Each error
names the line.

**Where examples live.** Beside the template:
`internal/template/builtin/<name>/examples.jsonl`, embedded with
`template.json` and `README.md`, or `.decide/templates/<name>/examples.jsonl`
for a project. With no file named, eval uses the template's own. An
offline test checks each built-in file against its template, as `TestDemo`
checks the demo.

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

Score also shows agreement within one level, and the mean distance of
`Score`, the expected level, from the label.

**Exact level is the default for score**: one rule for choice and score,
and the one `calibrate` uses (`checkDataset` predicts the most likely
level), so agreement and the fit agree on what a mistake is. Rejected:
rounding `Score`, an expectation, so an answer split between levels 0 and
4 rounds to 2, which the model thinks unlikely. Rejected: within one,
which on a three level scale passes nearly everything.

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
50 asked, all answered by jev-1.13.0

Agreement
  destructive   48 of 50   96%
  severe        47 of 50   94%   1 unsure
  leak          44 of 50   88%

Flags                         misses     false alarms
  severe   yes >= 80%         1 of 20    2 of 30
           suggested yes > 71%           held out: 0 of 10 missed, 3 of 15 false alarms
  leak     yes >= 80%         2 of 6     0 of 44
           no suggestion: 6 labeled yes, need 20

Confident mistakes: wrong at 80% or more
  examples.jsonl:7   severe   said no at 95%, labeled yes
                     rm -rf ~/
```

**Flags.** eval applies each flag's conditions to the label as a certain
answer: probability 1 for the labeled answer and 0 for the rest, or the
labeled level for a score. An example the flag would then mark is a
positive. A miss is a positive decide did not flag; a false alarm is a
negative it did. One rule covers noul, choice and score flags, and flags
with several conditions.

**Confident mistakes.** An answer wrong by section 2 whose chosen answer
had probability 0.8 or more. That is `gate.MeasureTop`, the number decide
prints. The 0.8 is fixed in the first PR and follows no flag:
`command-risk` flags at 80%, `security` at 50% and 90%, and a bare `yes`
at 60% (`DefaultFlagAt`). Open question 2 asks whether it should. They are listed most confident first, with the
input passed through `clean`.

**Suggested thresholds** reuse `calibrate.Fit`, for flags with one noul
condition. For `yes >= X`, eval calls Fit with `Measure: noul`,
`NoulTarget: "false"`, `MaxError: 0`, `MinAllowed: 1` and `ECEBins: 10`.
Fit then reads "allowed" as not flagged and "wrong" as labeled yes, so it
picks the highest cutoff that misses no labeled yes in the fit split. The
suggestion is `yes > cutoff`, rounded down to a whole percent so every
fit positive stays flagged. A `no` flag is the mirror, with
`NoulTarget: "true"`. Rounding flags more examples, so Fit's `Baseline`
describes another rule: 0.719 shown as `yes > 71%` can turn zero false
alarms into ten. eval takes only the cutoff from Fit, then grades the
rule it shows on each split, as it grades the template's flag. Bounding
misses first is the issue's own trade: "catch every dangerous command,
accept 3 false alarms." A miss lets the danger through; a false alarm
costs attention.

**Splits.** An example goes to fit or held out by the parity of a hash of
its input as canonical JSON, with keys sorted and no spacing, so adding or
reordering examples, or reformatting an object input, never moves an old
one. The duplicate check in section 1 compares the same form. The
held out split never changes the cutoff; it only reports it.

**When to show one.** Only when each split holds at least 10 positives and
10 negatives for the flag, about 20 of each overall. Below that, one miss
moves the held out rate by more than 10 points, and `examples/calibrate`
already warns that six cases are too few. Otherwise the report says how
many more labels it needs. `ErrNoFit` means no cutoff lets one fit
example through without a labeled yes: the lowest scoring example is a
labeled yes or ties one, so any cutoff that leaves one example unflagged
misses a yes. Answers on mixed versions get no suggestion (section 5).

Rejected: fitting and reporting on the same examples, which grades the fit
on its own answers. Rejected: a new fitter in the CLI, when Fit is built
and tested.

## 4. `--fail-under` and `--max-misses`

`--fail-under 0.9` exits with code 2 when any graded question's agreement
is below 0.9, a fraction from 0 to 1. It compares the lowest agreement
across questions and names each question below the bar.

A question needs at least 10 labeled examples before the gate counts it,
the same bar as each split in section 3: below that, one example moves
agreement by more than 10 points. With `--fail-under`, a question with
some labels but fewer than 10 is an error before any request, naming the
question and how many more it needs. Skipping it would pass a gate that
checked nothing, and exit 2 would read as a model failure. Without the
flag, the report marks it "too few labels". A question with no labels is
not graded and not gated.

Exit codes match `decide run`: 0 when all pass, 2 for `--fail-under` as
for `--fail-on`, 130 when stopped, and 1 for an error or for a run where
any example failed. An example with no answer can't be graded, and
counting it either way would move the number. So eval asks every example,
then, if any failed, prints no report, lists the failed examples and
exits 1, saying to run the same command again. The cache keeps the
answered ones, so the rerun asks only the failed.

**Agreement alone can pass a broken safety check.** With 99 safe
commands and one dangerous one, a model that always answers "safe" agrees
on 99% and passes `--fail-under 0.9`, having missed the only case that
matters. So `--max-misses N` exits 2 when any flag misses more than N
labeled positives, using the flag table's counts for the template's own
flags. A safety template's CI runs both. `--max-misses` is an error
before any request when the template has no flags, such as `relevance`,
or when a flag has no labeled positive, since either would pass by
checking nothing.

Nothing to grade is an error too. Before sending any request, eval exits
1 when the file has no examples, or when no example has a valid label for
any of the template's questions, and says which. So `--fail-under` can
never pass a run that graded nothing.

Rejected: the mean across questions, which lets one broken question hide.
Rejected for `--fail-under`: grading flags, which would fail CI when only
a threshold moved. Flags have their own gate, `--max-misses`.
Rejected: a bar per question in the first PR.

## 5. Cache, cost and output

**Cost.** One request per example, with all the template's questions in
it: 50 examples is 50 requests. eval asks
before running more than `confirmAbove` (100), as run does, and
`--dry-run` prints the count and how many the cache answers.

**Cache.** eval creates a saved run with `runs.Create` and runs it with
`UseCache` and `Execute`, as `decide run` does. So the cache is on by
default and `--no-cache` asks fresh. A rerun after a threshold change
sends nothing, and rewording one question asks only that question again,
since its text is in the key. So eval is cheap on every push, with the
frozen answers a CI gate wants.

That cheapness has a limit. Changing the provider or model name already
changes the cache key, so those ask fresh. What a cached eval misses is
drift behind unchanged settings: the provider updating the model behind
`jev-latest`. Only `--no-cache` measures the model as it is today. The docs recommend
cached evals on each push and a `--no-cache` eval on a schedule or
before a release.

**Resume.** Not in the first PR. A saved run keeps no `--fail-under` and
`runs resume` reports as run does, so a resumed eval would lose its CI
gate. eval marks its run `"kind": "eval"`, and `runs resume` refuses it
and says to rerun the eval, which the cache makes cheap.

**Model version.** Fit needs one resolved version, such as `jev-1.13.0`.
A cached `Result` has no `Model`: `cache.Lookup` returns no version and
reuses answers whose version is unknown, and an alias can move mid run.
So eval suggests thresholds only when every answer was asked in this run
and names the same version, which it passes to Fit. Otherwise it grades
agreement, names the versions seen, and suggests `--no-cache`.

**Formats.** Text, and `--json`, which prints one object, since the report
is a summary, not a line per item. It is printed unchanged, as run's is.

```json
{"template": "command-risk", "examples": "examples.jsonl", "run": "20261020-1",
 "provider": "typesafe", "model": "jev-latest", "count": 50, "cached": 0,
 "questions": {"severe": {"type": "noul", "graded": 50, "agree": 47,
   "unsure": 1, "agreement": 0.94, "confident_mistakes": 1}},
 "flags": {"severe": {"flag": "yes >= 80%", "positives": 20, "negatives": 30,
   "misses": 1, "false_alarms": 2,
   "suggested": {"flag": "yes > 71%", "version": "jev-1.13.0",
     "fit": {"misses": 0, "false_alarms": 2},
     "held_out": {"misses": 0, "false_alarms": 3}}, "reason": ""}},
 "mistakes": [{"id": "examples.jsonl:7", "question": "severe", "label": true,
   "answer": "no", "probability": 0.95, "confident": true}],
 "fail_under": 0.9, "below": [], "max_misses": 0, "over": ["severe"]}
```

Each count is for the rule in `flag`. `unsure` counts answers already
outside `agree`; only noul questions have it. Score questions add
`within_one` and `mean_distance`.

## 6. Not in the first PR

- Examples for every built-in. The first PR ships `command-risk`'s,
  seeded from `demo/commands.txt`, whose README says which lines are
  flagged and which are severe. That is 11 commands with 3 severe, a seed
  only: too few for a suggestion, which needs 20 positives and 20
  negatives. The file grows with hard cases, such as dangerous commands
  that look harmless, each label checked by a second person. Each other
  template follows in its own PR.
- Suggested thresholds for choice and score. Fit takes only certainty
  measures for them, not the probability of one option.
- Writing a suggestion back into `template.json`.
- Misses and false alarms for matches, as `relevance` uses.
- Image templates, such as `receipt-quality`.
- `--format md`, `csv` and `github`. Markdown, for pull requests, is next.
- Comparing two template versions or models. `calibrate.Compare` does it
  from Go today.
- An eval API in the `templates` package.
- Examples too large to send whole, resuming an eval, and Fit's
  `Artifact` in `--json`, which describes a cutoff eval does not show.
- Suggestions from cached answers, which need `cache.Lookup` to return
  each answer's version.

## Open questions

1. Exact level as the default for score, or within one?
2. Should "confident" follow each flag's threshold instead of a fixed 0.8?
3. Is 10 positives and 10 negatives per split the right bar, and is an
   even split right for small files?
4. Bound misses for every flag, or let a template say which mistake costs
   more? Should `--max-misses` take a rate as well as a count?
5. Should evals, marked `"kind": "eval"`, appear in `decide runs list`?
6. The root package's `decide.Eval` asks one question. Is `decide eval`
   clear beside it, or is `decide grade` better?
7. Should `decide templates new --from` copy `examples.jsonl` too?
