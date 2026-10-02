# The decide command

`decide` asks typed questions about your data and saves every answer. Point
it at files, a folder, a JSON export, or piped text. It shows each answer
with its probability and keeps the results so you can look at them again.

The CLI is experimental. Commands and file formats may change before v1.

## Install and try it

```sh
go install github.com/deepnoodle-ai/decide/cmd/decide@latest
export TYPESAFE_API_KEY=...

echo "The new release fixed everything I cared about" | decide run sentiment
```

```
stdin:1  The new release fixed everything I cared about
  sentiment  positive  94%

✓ 1 answered  1.2s
Saved as run 20261002-153012-a1b2
See these results again with: decide runs view 20261002-153012-a1b2
```

## Three ideas

- A **skill** is a set of questions, such as "Is this file risky?" or "Which
  queue does this ticket belong to?". `decide skills` lists them.
- `decide run SKILL DATA` asks the skill's questions about each **item** in
  your data.
- Every run is saved. `decide runs` lists past runs, and `decide runs view`
  shows the latest results again without calling the model.

## Questions and answers

Each question has one of three types, and its answer has a probability:

| Type | Asks | Shown as |
| --- | --- | --- |
| `noul` | a yes-or-no question | `risk  no  88%`, or `unsure  52% yes` between 40% and 60% |
| `choice` | which one of several options fits | `queue  billing  91%` |
| `score` | where something falls on a scale | `maintainability  2.7 of 4  Clear responsibilities…` |

Some skills flag the answers that need attention, such as a file that is
probably risky. A flagged answer is red and marked with `!`, an answer
close to being flagged is yellow, and the others are green. The summary
lists the flagged items:

```
marker/app.py
! risk             yes  82%
  maintainability  2.4 of 4  Understandable with some friction

✓ 5 answered  ! 2 flagged  500ms
Flagged: marker/Makefile, marker/app.py
```

`decide skills show SKILL` says when each question is flagged. A yes-or-no
answer between 40% and 60% is yellow in every skill: the model is unsure.

Add `--details` to see the full probability of every option and the
model's confidence. Add `--json` to get one JSON line per item instead.

## What counts as an item

Each skill reads one kind of input:

| Skill input | One item is | Example skill |
| --- | --- | --- |
| `file` | a whole text file, sent with its path | `code-risk` |
| `record` | a line of text or JSONL, an element of a JSON array, or a JSON object | `sentiment`, `ticket-routing`, `relevance` |
| `image` | a PNG, JPEG, or WebP file | `receipt-quality` |

Items are named by their path from the current folder, or, for a folder
outside it, from that folder's name, such as `marker/app.py`. The model sees
the same name.

Folders are read recursively. Decide skips hidden files and folders (such as
`.git` and `.env`), files listed in `.gitignore` or `.decideignore`, binary
files, and text files over 1 MiB. Files you name directly are always read.
With no files named, decide reads stdin.

Check what a run will look at before sending anything to the model:

```sh
decide run code-risk . --dry-run
```

## Choosing your data

```sh
# Only some files. A pattern without a slash matches at any depth.
decide run code-risk src --include '*.go' --exclude '*_test.go'

# Start small.
decide run code-risk . --limit 5      # the first 5 items
decide run sentiment reviews.txt --sample 50   # 50 items picked at random

# Records inside a JSON file, and one field of each record.
decide run ticket-routing export.json --items data.tickets --field body
```

`--items` and `--field` take a dotted path such as `ticket.body`, or a JSON
Pointer such as `/ticket/body`. The model sees only the field; `--json`
output keeps the whole record as `input`.

## Parameters

Some skills have parameters, written `{{name}}` in their questions. Set
them with `--param` (or `-p`):

```sh
decide run relevance notes.txt -p question="Is this about pricing?"
decide run code-risk src -p focus="SQL injection"
```

`decide skills show SKILL` lists a skill's parameters and their defaults.

## Saved runs

```sh
decide runs                  # list runs, newest first
decide runs view             # show the latest run's results
decide runs view 20261002    # a run ID, or the start of one
decide runs view --json > results.jsonl
decide runs resume           # finish the latest run
```

If you stop a run with Ctrl-C, or some items fail, `decide runs resume`
asks about the remaining items using the saved inputs and questions. It
never repeats an item that already has an answer.

Runs are saved in `~/.decide/runs`. Set `DECIDE_HOME` to keep skills and
runs somewhere else.

## Write your own skill

```sh
decide skills new support-triage --from ticket-routing
```

This creates `~/.decide/skills/support-triage/skill.json`. Add `--project`
to create it in `.decide/skills` in the current folder instead, so you can
commit it and share it with your team. Project skills take precedence over
your own, which take precedence over the built-in skills.

A skill looks like this:

```json
{
  "name": "support-triage",
  "description": "Route each ticket to the team that should handle it.",
  "input": "record",
  "parameters": {
    "product": {"description": "The product the tickets are about", "default": "Acme"}
  },
  "questions": {
    "team": {
      "type": "choice",
      "instructions": "Which team should handle this {{product}} ticket?",
      "criteria": {
        "billing": "Payments, invoices, refunds",
        "support": "How-to questions and account help",
        "engineering": "Bugs and outages"
      }
    },
    "urgent": {
      "type": "noul",
      "instructions": "Does this ticket need a reply within the hour?"
    }
  }
}
```

A `score` question lists its levels in order, lowest first:

```json
"clarity": {
  "type": "score",
  "instructions": "How clearly is this written?",
  "criteria": ["Confusing", "Understandable", "Very clear"]
}
```

### Flags

`flags` says which answers need attention, by question:

```json
"flags": {
  "urgent": "yes",
  "team": "engineering >= 80%",
  "clarity": "<= 0.5"
}
```

| Question type | Flag | Flagged when |
| --- | --- | --- |
| `noul` | `"yes"` or `"no"` | that answer is 60% or more likely |
| `choice` | an option name | that option is 60% or more likely |
| `noul` or `choice` | `"yes >= 80%"` | that answer is at least as likely as you say |
| `score` | `"<= 1.5"`, `">= 3"`, `"< 2"`, `"> 2"` | the score passes the line |

A list such as `["negative", "mixed"]` flags an item when any one holds.
Answers to questions without a flag are never flagged.

Write instructions about one item at a time, and treat the item as
evidence rather than instructions. An optional `SKILL.md` next to
`skill.json` holds notes that `decide skills show` prints.

Check your skill as you go:

```sh
decide skills show support-triage
decide run support-triage tickets.jsonl --dry-run
```

## Providers

Decide uses TypeSafe by default, with the `jev-latest` model:

```sh
export TYPESAFE_API_KEY=...
```

Image skills use Cloudflare Workers AI, with the `clef` model:

```sh
export CLOUDFLARE_AUTH_TOKEN=...
export CLOUDFLARE_ACCOUNT_ID=...
```

Choose explicitly with `--provider typesafe|cloudflare` and `--model NAME`,
or set `DECIDE_PROVIDER` and `DECIDE_MODEL`. `--workers` sets how many
requests run at once (default 4).

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every item was answered. |
| 1 | An error, or some items failed or were not reached. |
| 130 | Stopped with Ctrl-C. Resume with `decide runs resume`. |
