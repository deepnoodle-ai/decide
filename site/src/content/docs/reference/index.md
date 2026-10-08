---
title: Reference
description: How decide reads your data, what it prints, and every command and flag.
---

New here? Start with the [quickstart](/start/).

`decide` asks typed questions about your data and saves every answer. Point
it at files, a folder, a JSON export, or piped text. It shows each answer
with its probability, and keeps the results so you can look at them again.
The CLI is experimental. Commands and file formats may change before v1.

## Three ideas

- A **template** is a set of questions, such as "Is this file risky?" or
  "Which queue does this ticket belong to?". `decide templates` lists them.
- `decide run TEMPLATE DATA` asks the template's questions about each
  **item** in your data.
- Every run is saved. `decide runs` lists past runs, and `decide runs view`
  shows the latest results again without calling the model.

## Reading the output

Each question has one of three types, and its answer has a probability:

| Type | Asks | Shown as |
| --- | --- | --- |
| `noul` | a yes-or-no question | `risk  no  88%`, or `unsure  52% yes` between 40% and 60% |
| `choice` | which one of several options fits | `queue  billing  91%` |
| `score` | where something falls on a scale | `maintainability  ━━━━━━━━────  2.7 of 4  Clear responsibilities…` |

Some templates flag the answers that need attention, such as a file that is
probably risky. A flagged answer is red and marked with `!`, an answer
close to being flagged is yellow, and the others are green. When a run has
more than one item, the summary lists the flagged ones:

```text
marker/app.py
! risk             yes           82%
  maintainability  ━━━━━━━─────  2.4 of 4  Understandable with some friction

✓ 5 answered  ! 2 flagged  500ms
Flagged: marker/Makefile, marker/app.py
```

Other templates look for matches instead, such as the items relevant to
your question. A match is bold green and marked with `●`, the other
answers are dim, and the summary lists the matches when there is more than
one item:

```text
notes.txt:12  Annual plans are now 20% cheaper than monthly ones
● relevant  yes     91%

✓ 30 answered  ● 4 matched  1.1s
Matched: notes.txt:12, notes.txt:15, notes.txt:22, notes.txt:28
```

An answer to a question with no flag or match is shown in cyan: it is a
value, not a verdict. `decide templates show TEMPLATE` says when each
question is flagged or matched. A yes-or-no answer between 40% and 60% is
yellow in every template: the model is unsure.

In a terminal, decide shortens a long description or preview with `…` so
each line fits the window. Piped output keeps every line whole. Add
`--details` to see the full probability of every option and the model's
confidence, or choose another [output format](/reference/output/).

## Pages

| Page | Covers |
| --- | --- |
| [Commands and flags](/reference/cli/) | every command and flag, saved runs, exit codes, and environment variables |
| [Items](/reference/items/) | what one item is in each kind of data, and choosing what to read |
| [Diffs](/reference/diffs/) | judging what changed, by hunk, function, file, or line |
| [Templates](/reference/templates/) | the built-in templates, and writing your own |
| [Providers](/reference/providers/) | Jev, Clef, GPT-6 Luna, and your own service |
| [Output formats](/reference/output/) | JSON, CSV, Markdown, and GitHub annotations |
| [The answer cache](/reference/cache/) | when decide asks again, and when it doesn't |
| [Claude Code](/reference/claude-code/) | the plugin, the audit, and the hunt |

For the Go library, see the
[package documentation](https://pkg.go.dev/github.com/deepnoodle-ai/decide).
