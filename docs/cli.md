# The decide command

`decide` asks typed questions about your data and saves every answer. Point
it at files, a folder, a JSON export, or piped text. It shows each answer
with its probability and keeps the results so you can look at them again.

The CLI is experimental. Commands and file formats may change before v1.

## Install and try it

```sh
brew install deepnoodle-ai/tap/decide
# or: go install github.com/deepnoodle-ai/decide/cmd/decide@latest
export TYPESAFE_API_KEY=...   # or use Clef; see Providers below

echo "The new release fixed everything I cared about" | decide run sentiment
```

```
Running sentiment on 1 line · typesafe jev-latest

stdin:1  The new release fixed everything I cared about
  sentiment  positive  94%

✓ 1 answered  nothing flagged  1.2s
Saved as run 20261002-153012-a1b2
See these results again with: decide runs view 20261002-153012-a1b2
```

Each [release](https://github.com/deepnoodle-ai/decide/releases) also has
binaries for Linux, macOS, and Windows, with a `checksums.txt` file.
`decide --version` prints the version you have.

## Three ideas

- A **template** is a set of questions, such as "Is this file risky?" or "Which
  queue does this ticket belong to?". `decide templates` lists them.
- `decide run TEMPLATE DATA` asks the template's questions about each
  **item** in your data.
- Every run is saved. `decide runs` lists past runs, and `decide runs view`
  shows the latest results again without calling the model.

## Questions and answers

Each question has one of three types, and its answer has a probability:

| Type | Asks | Shown as |
| --- | --- | --- |
| `noul` | a yes-or-no question | `risk  no  88%`, or `unsure  52% yes` between 40% and 60% |
| `choice` | which one of several options fits | `queue  billing  91%` |
| `score` | where something falls on a scale | `maintainability  ━━━━━━━━────  2.7 of 4  Clear responsibilities…` |

Some templates flag the answers that need attention, such as a file that is
probably risky. A flagged answer is red and marked with `!`, an answer
close to being flagged is yellow, and the others are green. The summary
lists the flagged items:

```
marker/app.py
! risk             yes           82%
  maintainability  ━━━━━━━─────  2.4 of 4  Understandable with some friction

✓ 5 answered  ! 2 flagged  500ms
Flagged: marker/Makefile, marker/app.py
```

Other templates look for matches instead, such as the items relevant to your
question. A match is bold green and marked with `●`, the other answers are
dim, and the summary lists the matches:

```
notes.txt:12  Annual plans are now 20% cheaper than monthly ones
● relevant  yes     91%

✓ 30 answered  ● 4 matched  1.1s
Matched: notes.txt:12, notes.txt:15, notes.txt:22, notes.txt:28
```

`decide templates show TEMPLATE` says when each question is flagged or
matched. A yes-or-no answer between 40% and 60% is yellow in every template:
the model is unsure.

In a terminal, decide shortens a long description or preview with `…` so
each line fits the window. Piped output keeps every line whole.

Add `--details` to see the full probability of every option and the
model's confidence. Add `--json` to get one JSON line per item instead, or
choose another [output format](#output-formats).

## What counts as an item

Your data decides what one item is:

| Data | One item is |
| --- | --- |
| JSONL, JSON, or CSV | each record: a line, an element of an array, or a row named by the header |
| a `.txt` file, or piped text | each line |
| any other text file, such as Markdown or code | the whole file, sent with its path |
| images, for an image template such as `receipt-quality` | each image |
| a diff, such as `git diff` output or a `.patch` file | each hunk: a block of changed lines |

Choose another unit with `--each`:

| `--each` | One item is | Named like |
| --- | --- | --- |
| `file` | the whole file, even a dataset | `notes.txt` |
| `section` | the text under each Markdown heading | `README.md#install` |
| `paragraph` | each paragraph or list item | `CHANGELOG.md:13` |
| `function` | each function or method in Go, Python, JavaScript, TypeScript, or Java | `models.py#L88  User.save` |
| `line` | each line | `notes.txt:4` |
| `hunk` | each block of changed lines in a diff | `server.go:42` |

```sh
decide run relevance docs -p question="pricing"                    # which docs?
decide run relevance docs --each section -p question="pricing"     # which sections?
decide run code-risk src --each function                           # which functions?
```

The model sees each section or paragraph with the headings above it. A
Markdown file's headings and code blocks are not paragraphs. Some templates
choose a unit for you: `code-risk` reads whole files.

With `--each function`, each function, method, and constructor is an item,
named by its line and its name, such as `User.save`. The model sees it with
the comments right above it, the file's imports, and the line that starts
its class. In JavaScript and TypeScript, a top-level statement that holds a
function, such as `app.get("/users", ...)`, is an item, and each test in a
`describe` block is an item, such as `parser › it "reads a header"`. Code
outside functions, such as constants, is not judged. Files in other
languages are skipped. When decide cannot follow a file's structure, the
whole file is one item, with a warning.

An item too long to judge in one request, about 64 KB of text, is judged
in parts, and the parts' answers are combined into one. A source file is
cut between its functions, and a function too long for one request is cut
at blank lines. An item is flagged
when any part is, and the answer shows the lines of that part. Answers
without a flag or match are averaged across the parts:

```
src/server.go  judged in 3 parts
! risk             yes           88%  lines 412-655
```

A record too long for one request, such as a CSV row with a long field,
is judged in parts the same way, and the answer names the part, such as
`part 2 of 3`.

With `--json`, such an item is still one line, with `parts` set to the
number of parts and `where` giving, for each flagged or matched question,
the part that decided it, such as `"lines 412-655"`.

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

A dry run also says how many answers the
[answer cache](#the-answer-cache) already holds.

## Diffs

Pipe in a diff to judge what changed rather than whole files:

```sh
git diff main | decide run code-risk --each function # which changed functions are risky?
git diff main | decide run code-risk --each hunk     # which changes?
gh pr diff 42 | decide run code-risk                 # which changed files?
git diff main | decide run prompt-injection          # instructions hidden for AI agents?
git show HEAD | decide run sentiment --each line     # each added line
decide run code-risk change.patch --fail-on flagged  # fail CI on a risky change
```

Decide reads a diff from `git diff`, `git show`, `git format-patch`,
`gh pr diff`, or `diff -u`, and any file that ends in `.diff` or `.patch`.
In a diff, one item is:

| `--each` | One item is | Named like |
| --- | --- | --- |
| `hunk` (the default) | each block of changed lines | `server.go:42  func (h *Handler) Delete(id string) error {` |
| `function` | each function a change touches, whole | `server.go#L40  Handler.Delete` |
| `file` | every change to one file | `server.go` |
| `line` | each added line | `server.go:43` |

A template that reads whole files, such as `code-risk`, judges each changed
file. Add `--each hunk` to judge each change on its own.

`--each function` judges each changed function in Go, Python, JavaScript,
TypeScript, or Java as a whole, which gives the model the code around a
change. The model sees the function as it is now, with added lines marked
`+` and removed lines shown as `-`, along with the file's imports. Decide
reads the function from the file on disk, so run it in the repository the
diff came from, at the version it describes. When the file is not there,
such as for `gh pr diff` of another branch, that file is judged by hunk,
with a warning. A change outside any function, such as to imports or a
deleted function, is judged as a hunk. Decide reads only files inside the
current folder or its git repository, whatever paths the diff names.

An item is named by the file and the first changed line in the new
version, followed by the function git found above the change, if any. The
model sees the change in diff form, with `-` for removed lines and `+` for
added ones, and whether the file was added, modified, or renamed.

Decide skips deleted files, binary files, lockfiles such as `go.sum` and
`package-lock.json`, and generated files that start with a `Code generated
... DO NOT EDIT.` or `@generated` comment, and says which. Decide looks for
that comment in the diff, and in the file on disk when the diff's change is
further down. For a patch of files that are not on disk, leave out generated
files with `--exclude`. `--include` and
`--exclude` match the paths in the diff, and a lockfile or generated file
that `--include` names is judged.

A diff with nothing to judge, such as an empty one from `git diff` when
nothing changed, or one that changes only lockfiles, is not an error:
decide says so and exits 0, so a CI gate on that change passes.

When a diff holds several commits, as from `git log -p` or `git
format-patch`, each item's name ends with its commit, such as
`server.go@1a2b3c4:42`. Decide does not read the combined diff that `git
show` prints for a merge commit; diff against one side of it instead, such
as `git diff main...feature`.

A `.diff` or `.patch` file that you name is read as a diff. In a folder,
such files are read as diffs only with `--each hunk`, and their items are
named after the patch, such as `fixes/auth.patch: server.go:42`.

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

Some templates have parameters, written `{{name}}` in their questions. Set
them with `--param` (or `-p`):

```sh
decide run relevance notes.txt -p question="pricing"
decide run code-risk src -p focus="SQL injection"
```

`decide templates show TEMPLATE` lists a template's parameters and their
defaults.

## Saved runs

```sh
decide runs                  # list runs, newest first
decide runs view             # show the latest run's results
decide runs view 20261002    # a run ID, or the start of one
decide runs view --json > results.jsonl
decide runs view --top 20    # the 20 items nearest their flags
decide runs resume           # finish the latest run
```

`--top` ranks items by how far their likeliest flagged answer is above
or below its flag's threshold, so flagged items come first and the rest
follow by how near they came. It uses matches when a template has no
flags, leaves out items that failed, and works with text, JSON, and CSV
output. Use it to read a long run from the top, such as `security` on a
whole repository, where the ranking says where to look.

If you stop a run with Ctrl-C, or some items fail, `decide runs resume`
asks about the remaining items using the saved inputs and questions. It
never repeats an item that already has an answer.

Runs are saved in `~/.decide/runs`. Set `DECIDE_HOME` to keep templates and
runs somewhere else.

## The answer cache

Decide keeps every answer it gets, and asks again only about what changed.
Run a template a second time over the same files and nothing is sent:

```
✓ 41212 answered  ! 3 flagged  48s
  81974 answers from cache · 450 asked
```

The line counts answers, one for each question about each item, so an
item can have some answers from the cache and others asked.

An answer is reused only for the same question about the same text, sent
to the same provider and address, with the same model name. Each question
is kept on its own, so adding a question to a template asks only that one.
A question is known by its key and its full definition, so changing its
wording or options, or renaming its key, asks it again. Flags and
`--fail-on` are worked out from the answers on each run, so changing a
threshold asks nothing again.

With `--json`, each result lists in `cached` the questions whose answers
came from the cache. `request_id` and `model` describe the request the run
sent for the item, and are left out when every answer came from the
cache.

`--dry-run` says how many answers the cache already holds:

```
41212 items · 81974 answers in the cache · 450 to ask
```

The cache is keyed by the model name you ask for, such as `jev-latest` or
`clef`. Each answer also notes the model version that gave it, when the
provider names one. When a live answer shows that the model behind the
name changed, older answers are asked again, including the cached answers
of the item that showed it. A run where everything is cached sends no
request, so it can't see an upgrade: run with `--no-cache` to ask fresh,
or name an exact version, such as `--model jev-1.13.0`, to pin answers to
it. Workers AI names no version, so Clef answers stay until `--no-cache`.

`--no-cache` asks every question again, and keeps the new answers in place
of the old. `runs resume` keeps the setting of the run it resumes.

The cache is the folder `~/.decide/cache`, readable only by you. It holds
hashes and answers, never your text, file names, or images. Several decide
processes can use it at once. Delete the folder to clear it. If it can't be
read or written, decide warns and asks every question.

## Write your own template

```sh
decide templates new my-routing --from ticket-routing
```

This creates `~/.decide/templates/my-routing/template.json`. Add `--project`
to create it in `.decide/templates` in the current folder instead, so you can
commit it and share it with your team. Project templates take precedence over
your own, which take precedence over the built-in templates.

A template looks like this:

```json
{
  "name": "my-routing",
  "description": "Route each ticket to the team that should handle it.",
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

A template reads text unless it says `"input": "image"`. Add `"each": "file"`
(or `section`, `paragraph`, `function`, or `line`) to choose the unit its
questions are written for; `--each` still overrides it.

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

`matches` takes the same conditions and marks the answers someone is
looking for, such as the items relevant to a topic:

```json
"matches": {"relevant": "yes"}
```

Write instructions about one item at a time, and treat the item as
evidence rather than instructions. An optional `README.md` next to
`template.json` holds notes that `decide templates show` prints.

Check your template as you go:

```sh
decide templates show my-routing
decide run my-routing tickets.jsonl --dry-run
```

## Providers

Decide runs on two decision models, through the same commands and
templates:

- **Jev** by [TypeSafe](https://docs.typesafe.ai/introduction), the
  default, with the `jev-latest` model:

  ```sh
  export TYPESAFE_API_KEY=...
  ```

- **Clef** by [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/models/clef/),
  with the `clef` model, or `clef-flash` for lower latency. Image templates
  always use Clef.

  ```sh
  export CLOUDFLARE_AUTH_TOKEN=...
  export CLOUDFLARE_ACCOUNT_ID=...
  export DECIDE_PROVIDER=cloudflare
  ```

Any other service that speaks the Jev API works too, such as one you host
yourself. Point decide at it with `TYPESAFE_BASE_URL`, along with
`TYPESAFE_API_KEY` and a model name:

```sh
export TYPESAFE_BASE_URL=https://decisions.example.com
decide run sentiment notes.txt --model my-model
```

Choose for one run with `--provider typesafe|cloudflare` and `--model NAME`,
or for every run with `DECIDE_PROVIDER` and `DECIDE_MODEL`. `--workers` sets how many
requests run at once (default 4).

## Output formats

`--format` chooses how `run`, `runs view`, and `runs resume` print results
on stdout. The summary still goes to stderr.

| `--format` | Prints |
| --- | --- |
| `text` (the default) | answers for people to read, as shown above |
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
more. [Recipes](recipes.md) has workflows to copy.

`runs resume` prints the items answered before the run stopped as well
as the new ones, so its output covers the whole run in every format.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every item was answered, or a diff had nothing to judge. With `--fail-on`, none was flagged or matched. |
| 1 | An error, or some items failed or were not reached. This wins over 2, even if items were flagged. |
| 2 | With `--fail-on`, at least one item was flagged or matched. |
| 130 | Stopped with Ctrl-C. Resume with `decide runs resume`. |

Add `--fail-on flagged` to stop a script or CI job when something needs
attention, or `--fail-on matched` when something you are looking for turns
up:

```sh
decide run code-risk src --fail-on flagged
```

An answer close to being flagged (yellow) does not count. The template's
`flags` or `matches` set the line: to fail at a different probability,
copy the template with `decide templates new` and change them. When a
run stops early, finish it with `decide runs resume --fail-on flagged` to
gate on the whole run.
