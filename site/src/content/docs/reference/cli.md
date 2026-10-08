---
title: Commands and flags
description: Every decide command and flag, saved runs, exit codes, and environment variables.
---

```text
decide run [flags] TEMPLATE [DATA...]
decide runs list | view [RUN] | resume [RUN]
decide templates list | show TEMPLATE | new NAME
```

`decide --help`, and `--help` on any command, prints the same flags.
`decide --version` prints the version you have.

## `decide run`

Asks a template's questions about each item in your data. With no files
named, it reads stdin.

```sh
decide run code-risk src --include '*.go' --limit 5
git diff main | decide run code-risk --each function
echo "This is great" | decide run sentiment
```

Over 100 items, it asks before it starts, unless you add `--yes`.

### Choosing the data

#### `--include`, `-i`

Reads only the files that match a pattern. A pattern without a slash
matches at any depth. Repeat it for more patterns.

```sh
decide run code-risk src --include '*.go' --include '*.py'
```

#### `--exclude`, `-x`

Skips the files that match a pattern. Repeatable.

```sh
decide run code-risk src --include '*.go' --exclude '*_test.go'
```

#### `--each`

Chooses what one item is: `file`, `line`, `paragraph`, `section`,
`function`, or `hunk`. The default depends on the data; see
[Items](/reference/items/).

```sh
decide run relevance docs --each section -p question="pricing"
```

#### `--items`

Says where the records are inside a JSON file, as a dotted path or a JSON
Pointer.

```sh
decide run ticket-routing export.json --items data.tickets
```

#### `--field`

Asks about one field of each JSON record, such as `body` or `ticket.body`.
The model sees only the field, and `--json` output keeps the whole record
as `input`.

```sh
decide run ticket-routing tickets.jsonl --field body
```

#### `--limit`, `-n`

Stops after this many items.

```sh
decide run code-risk . --limit 5
```

#### `--sample`

Picks this many items at random.

```sh
decide run sentiment reviews.txt --sample 50
```

### Asking

#### `--param`, `-p`

Sets a template parameter, as `name=value`. Repeatable.
`decide templates show TEMPLATE` lists a template's parameters and their
defaults.

```sh
decide run relevance notes.txt -p question="pricing"
```

#### `--dry-run`

Shows what a run would look at and how many answers the
[cache](/reference/cache/) already holds, without calling the model.

```sh
decide run code-risk . --dry-run
```

#### `--no-cache`

Asks every question again, and keeps the new answers in place of the old.

```sh
decide run code-risk src --no-cache
```

#### `--provider`

Chooses the model's provider for this run: `typesafe`, `cloudflare`, or
`openai`. See [Providers](/reference/providers/).

```sh
decide run sentiment notes.txt --provider cloudflare
```

#### `--model`, `-m`

Chooses the model by name. The default is the provider's own.

```sh
decide run sentiment notes.txt --model jev-1.13.0
```

#### `--workers`

Sets how many requests run at once. The default is 4.

```sh
decide run code-risk src --workers 8
```

#### `--yes`, `-y`

Starts a run of more than 100 items without asking first.

```sh
decide run code-risk . --yes
```

### Printing results

#### `--details`, `-d`

Shows the probability of every option, and the model's confidence.

```sh
echo "It's fine, I guess" | decide run sentiment --details
```

#### `--format`, `-f`

Prints results as `text`, `json`, `csv`, `md`, or `github`. See
[Output formats](/reference/output/).

```sh
decide run code-risk src --format csv > results.csv
```

#### `--json`

Prints one JSON line per item. It is short for `--format json`.

```sh
decide run code-risk src --json | jq .answers
```

#### `--fail-on`

Exits with code 2 if any item is `flagged`, or `matched`, so a script or CI
job can stop on the result. An answer close to being flagged doesn't count.

```sh
git diff main | decide run code-risk --each function --fail-on flagged
```

## Saved runs

```sh
decide runs                  # list runs, newest first
decide runs view             # show the latest run's results
decide runs view 20261002    # a run ID, or the start of one
decide runs view --top 20    # the 20 items nearest their flags
decide runs resume           # finish the latest run
```

Runs are saved in `~/.decide/runs`. Set `DECIDE_HOME` to keep templates
and runs somewhere else.

### `decide runs list`

Lists saved runs, newest first. `decide runs` alone does the same.

### `decide runs view`

Shows a run's results again, without calling the model. It takes a run
ID, or the start of one, and shows the latest run by default. It takes
`--details`, `--format`, and `--json` as `run` does.

#### `--top`

Shows only this many items: the flagged ones first, then those nearest a
flag.

```sh
decide runs view --top 15
```

`--top` ranks items by how far their likeliest flagged answer is above or
below its flag's threshold. It uses matches when a template has no flags,
and leaves out items that failed. Use it to read a long run from the top,
such as `security` on a whole repository, where the ranking says where to
look.

### `decide runs resume`

Finishes a run that stopped early or had failures, using the saved inputs
and questions. It never asks again about an item that already has an
answer, and it keeps the cache setting of the run it resumes. It takes
`--details`, `--format`, `--json`, `--workers`, and `--fail-on` as `run`
does.

```sh
decide runs resume --fail-on flagged
```

Its output covers the whole run, the items answered before it stopped as
well as the new ones, so `--fail-on` gates on the whole run.

## Templates

### `decide templates list`

Lists the templates you can run: your project's, your own, and the
built-in ones. `decide templates` alone does the same.

### `decide templates show`

Explains what a template asks, when it flags an answer, its parameters,
and how to run it.

```sh
decide templates show code-risk
```

### `decide templates new`

Creates your own template. See
[Write your own template](/reference/templates/#write-your-own-template).

#### `--from`

Starts from a copy of another template.

```sh
decide templates new my-routing --from ticket-routing
```

#### `--project`

Saves it in this folder's `.decide/templates`, to commit and share with
your team.

```sh
decide templates new my-routing --project
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every item was answered, or a diff had nothing to judge. With `--fail-on`, none was flagged or matched. |
| 1 | An error, or some items failed or were not reached. This wins over 2, even if items were flagged. |
| 2 | With `--fail-on`, at least one item was flagged or matched. |
| 130 | Stopped with Ctrl-C. Resume with `decide runs resume`. |

A template's `flags` or `matches` set where the line is. To fail at a
different probability, copy the template with `decide templates new` and
change them.

## Environment variables

| Variable | Sets |
| --- | --- |
| `TYPESAFE_API_KEY` | the key for Jev, the default provider |
| `TYPESAFE_BASE_URL` | another service that speaks the Jev API |
| `CLOUDFLARE_AUTH_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` | the token and account for Clef on Workers AI |
| `OPENAI_API_KEY` | the key for GPT-6 Luna |
| `OPENAI_BASE_URL` | another address for OpenAI's API |
| `DECIDE_PROVIDER` | the provider for every run: `typesafe`, `cloudflare`, or `openai` |
| `DECIDE_MODEL` | the model for every run |
| `DECIDE_HOME` | where templates, runs, and the cache live, in place of `~/.decide` |

See [Providers](/reference/providers/) for each provider's setup.
