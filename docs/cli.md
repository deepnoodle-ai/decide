# decide: a little judgment in your pipeline

Ask typed questions. Keep the evidence. Let shell tools do the routing.
`decide` (System One Decisions) reads JSONL and saves answers alongside the data.
Its commands, flags, and saved format are experimental.

## Take it for a spin

From this checkout, with Go 1.27+, Bash, jq, and `TYPESAFE_API_KEY` set:

```sh
go install ./cmd/decide
decide --help
bash scripts/cli-tour.sh
```

The [tour](../scripts/cli-tour.sh) creates tiny inputs, exercises every
command using live API answers, and saves everything in a fresh temporary
directory. [Recipes](cli-recipes.md) explain the dishes. No canned answers.

## Pick your tool

| Command | Try it on | What it does |
| --- | --- | --- |
| `judge` | Three questions about one ticket | Ask named Noul, Choice, and Score questions |
| `grep` | Find reports of lost work | Keep records meeting your Noul threshold |
| `label` | Billing or engineering? | Choose one option from your taxonomy |
| `score` | Vague wish or concrete proposal? | Judge against named, ordered rubrics |
| `pick` | Which address gets the receipt? | Choose a supplied candidate or abstain |
| `join` | Acme Tools meets ACME Tools LLC | Judge a relation for each supplied pair |
| `rank` | Rescue notes, best first | Sort saved Noul answers, highest first |
| `pack` | A small context suitcase | Select whole strings under a byte budget |
| `gate` | Allow, review, or escalate? | Apply a policy to saved answers |
| `check` | Put a judgment in CI | Ask questions, apply a policy, set an exit code |
| `eval` | Did the new model get worse? | Convert, fit, and compare labeled evidence |

`rank`, `pack`, `gate`, and every `eval` operation are offline. The other
commands call the API. Live commands read `TYPESAFE_API_KEY`,
`TYPESAFE_BASE_URL`, and `TYPESAFE_DEFAULT_MODEL`; `--model` overrides the
model. There is no API-key flag. Each command has `--help`.

## Ask once, reuse the answer

Save this as `questions.json`:

```json
{"relevant":{"type":"noul","instructions":"Does this passage explain restoring an interrupted software session?"}}
```

Then run in Bash with `set -o pipefail`:

```sh
printf '%s\n' 'Load the saved session ID and replay its history.' \
  'Mash two bananas into the cake batter.' \
  | decide judge --input text --questions questions.json --as rescue \
  > observed.jsonl
decide rank --run rescue --answer relevant < observed.jsonl \
  | decide pack --run rescue --answer relevant --budget-bytes 60 \
  | jq -r 'select(.runs[-1].result.selected) | .data'
```

Only `judge` calls the model. `--run rescue` selects the saved evidence even
after ranking appends another run. Pack counts UTF-8 string bytes, excluding
JSON encoding and output separators; it does not count tokens or summarize.

## Small files, explicit rules

`judge` and `check` take a native question map. `label` takes one Choice
definition; `score` takes named Score definitions. Instructions are required
and can be any JSON value, including null. The
[recipes](cli-recipes.md) supply runnable configurations for all of them.

Noul returns `noul`, the probability of yes, with no confidence field.
Choice keeps the selected option, distribution, and confidence. Score
keeps the ordered-level position, legend, distribution, and confidence.
Scores are judgments on described levels, not measured quantities.

Grep needs an explicit `--threshold`; it keeps failures as well as matches.
`--keep-all` retains nonmatches with a `matched` result. Label chooses one
option; add an `other` option yourself when it serves the task.

Pick expects `{"state":...,"candidates":[...]}` and returns the original
chosen item and its zero-based index. Abstention is always available;
empty candidates abstain without an API call. The current cap is 254
candidates plus abstention. Your code supplies candidates and offsets.

Join expects `{"left":...,"right":...}`. It judges supplied pairs; it
does not find candidate pairs, enforce unique matches, or track unmatched
records. Only left/right enter model state; extra source fields survive.

Gate uses [x/gate policies](../x/gate). It reports decisions and
reasons; caller code acts on them. Example cutoffs are illustrative.
Measure your own before using them to route real work.

## Read the envelope

Each line contains `typesafe_cli: 1`, `id`, `data`, and `runs`. Original
JSON stays in `data`, including large numbers. Each live run saves the
actual state and questions, raw response, requested and resolved model,
request ID, usage, and any error. Files contain source content too.

The `typesafe_cli` member identifies version 1 of the saved envelope.

```sh
jq '{id, data, model: .runs[-1].response.model,
     answers: .runs[-1].response.answers,
     error: .runs[-1].error}' observed.jsonl
```

`--as NAME` names a new run; names must be unique within a record. The
default name is the command. Offline commands use the latest run unless
you select `--run NAME`. A missing or failed run never silently falls back
to an older answer. Saved answers are revalidated against their questions.

An object's nonempty string `id` is retained; other records get `r1`, `r2`,
and so on. Supply stable IDs for evaluation. Ground-truth labels stay out
of model state with `--state-field state`; see the
[evaluation recipe](cli-recipes.md#did-it-get-worse).

## Finish cleanly

JSONL is the default: one JSON value per nonblank line. `--input text`
turns each line, including empty lines, into a string. Live state must be a
string, object, or array. Use jq to reshape records.

| Exit | Meaning |
| --- | --- |
| `0` | Completed; gate can legitimately report review or escalate |
| `1` | No grep matches, check rejected a record, or eval found a regression |
| `2` | Usage, input, evaluation, or output failure |
| `130` | Canceled |

Stdout carries JSON or requested help; stderr carries diagnostics. Output
can be partial on failure. Check the exit status and saved errors before
using a file as a completed run. Bash's `set -o pipefail` catches failures
earlier in a pipeline.

Live defaults are 32-record chunks, four workers, and a 1 MiB record limit.
Rank, pack, and dataset conversion collect at most 10,000 records or 64 MiB
by default. Flags let you change these limits; output stays in input order
except when ranking. Rank ties preserve input order; pack selection ties
prefer later strings and emit records in the supplied order.

See the [spec](specs/cli.md) for the full saved format and contracts.
