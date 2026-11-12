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
or `matched`, a column for each question, and any `error`.

The Markdown report lists flagged, matched, and failed items first, and
collapses the rest.

With `--format github`, each flagged item becomes a warning on its file
and line in the pull request, or an error when `--fail-on` names it. The
job's summary page has the full report.
[Review pull requests](/tutorials/review-pull-requests/) has a workflow to
copy.
