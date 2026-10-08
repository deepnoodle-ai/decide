---
title: Output formats
description: Print results as text, JSON, CSV, a Markdown report, or GitHub annotations.
---

`--format` chooses how `run`, `runs view`, and `runs resume` print results
on stdout. The summary still goes to stderr.

| `--format` | Prints |
| --- | --- |
| `text` (the default) | answers for people to read, as in [Reading the output](/reference/#reading-the-output) |
| `json` | one JSON line per item, with every probability; `--json` is short for this |
| `csv` | one row per item, for a spreadsheet |
| `md` | a Markdown report, for a pull request comment |
| `github` | an annotation for each flagged or matched item, for GitHub Actions |

```sh
decide runs view --format csv > results.csv
git diff main | decide run code-risk --each function --format md > report.md
```

A CSV row has the item's `source` and `status`, whether it was `flagged`
or `matched`, a column for each question, and any `error`. A yes-or-no
question's column holds the probability of yes, a score question's the
score, and a choice question's the choice, followed by its probability in
a column such as `queue_probability`. A cell that starts with `=`, `+`, `-`,
or `@` starts with `'`, so a spreadsheet does not read it as a formula.

The Markdown report lists flagged, matched, and failed items first, and
collapses the rest. Each table shows up to 100 items. When a diff has
nothing to judge, the report says so, so a pull request comment made from
it does not go stale.

With `--format github`, each flagged item becomes a warning on its file
and line, which GitHub shows on the pull request's changes, and each
matched item becomes a notice. The items that `--fail-on` names are errors
instead, since they fail the job. Run decide from the top of the
repository so the files' paths match. In a GitHub Actions job, decide also
adds the Markdown report to the job's summary page. GitHub shows at most
10 annotations of each kind for a step, so the summary is the place to see
more. [Review pull requests](/tutorials/review-pull-requests/) has a workflow to copy.

`runs resume` prints the items answered before the run stopped as well
as the new ones, so its output covers the whole run in every format.
