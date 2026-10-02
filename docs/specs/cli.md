# decide: experimental composable judgment CLI

> Historical record of the original implementation plan. Package promotion
> supersedes its experimental-package layout and graduation exclusions.
> The old `x/` paths below are historical, not current imports. Use
> `backend`, `cloudflare`, and `patterns/<name>` instead. See the
> [README](../../README.md) for current APIs and the pre-v1 policy.

Status: accepted
Date: 2026-10-01
PRD: [cli](../prds/decision-tools.md)
Design: [package boundaries](../design/decision-tools.md)

## Context

The SDK supplies typed judgments, validated answers, and experimental
composition packages. Shell workflows need input adapters, stable mapping
back to source records, and saved observations reusable without another API
call. jq and Miller already handle deterministic record transformations.
The CLI puts narrow judgment operations between those tools and preserves
enough evidence to apply a different policy later.

## Goals

- Provide all eleven commands in v0.
- Run live operations through one bounded, ordered record executor.
- Preserve source JSON values, IDs, prior runs, and unknown envelope fields.
- Revalidate saved answers before offline policy or collection operations.
- Exercise every command using fake HTTP and injected IO, without keys.

## Non-goals

No shell execution, generated prose, retrieval, candidate discovery,
implicit entity assignment, pairwise ranking, CLI schema stability,
automatic model spending, or changes to the stable SDK's public surface.

## Proposal

`cmd/decide` creates a cancellable context and delegates to `internal/cli`.
The internal package owns argument parsing, records, execution, saved-answer
validation, and workflow adapters. It consumes existing SDK packages and
the standard library; root code does not import it. The executable states
that its commands and saved format are experimental.

```text
stdin -> record reader -> live chunk runner -> named run -> stdout
                        \ offline adapter -> named run -> stdout
                             rank / gate / pack / eval
```

### Input and saved records

Default input is one JSON value per nonblank line. Each value is a record;
an explicit `--input text` adapter reads one line as a JSON string, including
empty lines. No auto-detection, shell interpolation, or expression evaluator.
Keep JSON numbers and original nested values through `json.RawMessage`.

Ordinary input becomes this envelope. Existing version-1 envelopes pass
through with their `data`, IDs, runs, and unrecognized top-level fields.
Objects carrying `typesafe_cli` are reserved envelopes: reject malformed
or unsupported versions rather than wrap them as ordinary data.
The discriminator identifies the envelope format independently of the
executable name.

```json
{
  "typesafe_cli": 1,
  "id": "ticket-17",
  "data": {"id":"ticket-17","text":"Payouts failed"},
  "runs": [{
    "name":"triage",
    "command":"judge",
    "state":{"id":"ticket-17","text":"Payouts failed"},
    "questions":{"urgent":{"type":"noul","instructions":"Urgent?"}},
    "response":{
      "model":"jev-1.13.0",
      "answers":{"urgent":{"type":"noul","noul":0.9}},
      "usage":{"input_tokens":100,"output_tokens":10}
    },
    "requested_model":"jev-latest",
    "request_id":"request-123"
  }]
}
```

An ordinary object's nonempty string `id` becomes the envelope ID; otherwise
use `r1`, `r2`, and so on by input position. Input `data.id` is preserved.
Envelope IDs remain unchanged through pipelines. Reject duplicate IDs
within a bounded collection. Positional IDs are stable only for that input
sequence; evaluation users must supply IDs stable across separate runs.

A run contains `name`, `command`, optional raw `state`, raw `questions`,
authoritative raw `response`, `requested_model`, `request_id`, optional
`error`, `invalid`, and workflow `result`. Preserve unrecognized run fields.
`response` alone holds wire answers, resolved model, and token usage; do not
duplicate those values in independently editable fields. Error metadata
survives because response JSON does not include SDK validation state.

`error` contains `kind`, `message`, and optional HTTP status/request ID.
`invalid` maps question keys to structured validation details. Save raw
partial responses even when a call returns an error. Distinguish input,
transport, HTTP, validation, cancellation, and output failures. No API key
or Authorization value may appear in either stream or saved metadata.

`--as NAME` names the appended run, defaulting to the command name. Reject
an empty or duplicate name before making the next call. `--run NAME`
selects an existing run; omitted means the last run, regardless of whether
it succeeded. Missing, failed, or invalid selected runs cannot fall back
to an earlier success. Offline result runs name their selected source run.

### Options and question files

Common record flags: `--input jsonl|text`, `--as NAME`, and
`--max-record-bytes N` (default 1048576, positive). Live flags:
`--model STRING`, `--workers N` (default 4), `--chunk-size N` (default 32).
Credentials and base URL come from the SDK environment; no API-key flag.
Collection flags: `--max-records N` (default 10000) and
`--max-bytes N` (default 67108864), both positive. Count actual input bytes.
The record cap also limits parsed envelopes with growing run histories.

`judge` and `check` accept `--state-field NAME` to select one top-level
`data` field as state. Omitted means the complete `data` value. Reject a
missing field. This permits fixtures with labels outside model context.
Pick and join use their prescribed input objects instead.

`--questions FILE` contains the native question map, without a wrapper:

```json
{
  "urgent":{"type":"noul","instructions":"Is the request urgent?"},
  "team":{"type":"choice","instructions":"Which team?",
          "criteria":{"billing":"Payments","engineering":"Bugs"}}
}
```

Decode via `decide.DecodeQuestion`; validate the request before HTTP.
Do not interpret question IDs as model instructions. String/object/array/
null instruction and criteria values follow the SDK's wire contracts.
Question files, taxonomy, rubric, and policy files are bounded JSON files.

### Exact command contracts

| Command | Required options | Operation and result |
| --- | --- | --- |
| `judge` | `--questions FILE` | Live mixed questions; raw response is the result evidence. |
| `grep` | `--question TEXT --threshold P` | Live Noul key `match`; result `matched` is `noul >= P`. |
| `label` | `--taxonomy FILE` | Live Choice key `label`; result contains `label`. |
| `score` | `--rubric FILE` | Live named Score questions; preserve full responses. |
| `rank` | `--answer KEY` | Offline selected-run Noul ordering; result `position`, `noul`, `source_run`. |
| `pick` | `--question TEXT` | Live supplied candidate Choice; result `picked`, `abstained`, optional `item`, `index`. |
| `join` | `--question TEXT` | Live Noul key `relation`; result `noul` describes the caller's relation. |
| `gate` | `--policy FILE` | Offline `gate.DecodeRule` and evaluate; result is the gate decision with `source_run`. |
| `pack` | `--answer KEY --budget-bytes N` | Offline byte selection; result `action`, `selected`, `size_bytes`, `cause`, `source_run`. |
| `check` | `--questions FILE --policy FILE` | Live judge plus policy; result is the gate decision. |
| `eval` | subcommand below | Offline calibration artifacts and dataset conversion. |

`grep --keep-all` emits every record with the match decision. Normally it
emits matches plus failed records, retaining failure evidence. Require a
finite threshold in [0,1]. Zero matches without operational failure exits
1; this does not mean the evaluation was unavailable.

Taxonomy file: `{"instructions":<JSON>,"criteria":{"key":<JSON>}}`.
No implicit `other` option. Rubric file is a map of named definitions:
`{"quality":{"instructions":<JSON>,"criteria":[<JSON>,<JSON>]}}`.
Every rubric becomes a Score question. Reuse SDK shape and cap validation.

Pick input `data` is `{"state":<JSON>,"candidates":[<JSON>,...]}`.
Use `x/pick` with candidates' raw JSON as original items, described by
objects containing each raw candidate under `value`. This supports scalar
candidates within the API's description union. Record indices and preserve supplied candidate offsets
as ordinary data. Abstention is mandatory. The current Choice limit leaves
254 candidate slots. An empty list returns `picked:false, abstained:true`
without HTTP, fabricated model metadata, or a fabricated answer. Store this
abstention in run metadata so gate maps it to `gate.Abstained`, including
the empty-list case. A nonempty pick saves its actual Choice question and
response. Other malformed input or excessive candidates exit 2.

Join input `data` is `{"left":<JSON>,"right":<JSON>}`. The supplied pair
is model state. Emit every pair observation. Producing candidate pairs,
turning probabilities into proposed matches, and inventorying unmatched
rows belong to caller tools. Neither a true relation nor its probability
implies a one-to-one assignment or a global entity cluster.

Rank loads the bounded collection, validates each selected Noul, and calls
`x/rank.Rerank`. Higher values rank first; equal values retain input order.
Output every envelope in ranked order. Reject missing/failed observations
before emitting a misleading partial order. It never asks again.

Pack requires original `data` strings. Use their UTF-8 byte lengths as
`compact.Segment.Size` and selected Noul as `compact.Scores.Needed`. Require
a positive `--budget-bytes`; use `compact.Rule{Budget:N}` with no floor,
pins, short forms, or verbatim replacement. Revalidate all scores before
selection; do not let the package's unscored fallback silently keep invalid
records. Emit every record with its `action`, `selected`, `size_bytes`, and `cause` in original order. Users
extract chosen strings with `jq -r 'select(.runs[-1].result.selected)|.data'`.
The byte total counts selected string bytes, excluding serialization and
any separator a later program adds. Ties follow `compact.Select`'s existing
more-recent-first rule; output order still follows source order.

Gate evaluates only selected-run inputs. Decode the saved request and
response, revalidate exact answer key sets and each answer against its
saved question, honor saved error/invalid metadata, and adapt abstention
explicitly. A policy may select its supplied missing-input outcome, but
an operational or validation failure still makes the process exit 2.
Gate decisions never execute actions. Offline policy files use the current
`x/gate` JSON format; CLI does not add a second policy language.

Check executes the same request runner as judge and evaluates its policy
against the new response. Emit observations and decision for every record.
Review or escalation exits 1; HTTP, parsing, validation, or other execution
failure exits 2 and takes precedence over semantic rejection.

### Calibration files

`eval fit --fit FILE --heldout FILE --config FILE` reads two existing
`calibrate.Dataset` objects and `calibrate.Config`, calls `calibrate.Fit`,
and writes one `calibrate.Artifact` JSON value. It makes no API calls.

`eval compare --artifact FILE --candidate FILE --tolerance FILE` reads the
existing artifact, candidate dataset, and `calibrate.Tolerance`, then writes
`calibrate.Comparison`. `ErrRegression` emits the comparison and exits 1;
invalid/mismatched datasets or unavailable metrics exit 2.

`eval dataset --answer KEY [--run NAME] [--label-field NAME]` converts a
bounded envelope stream to one `calibrate.Dataset`; label field defaults to
`label`, a top-level string in `data`. Each case uses the envelope ID,
selected-run exact saved state, selected raw answer, and supplied label.
Require one identical saved question/key and one resolved model throughout.
Revalidate observations; reject missing labels, duplicate IDs, mixed cohorts,
and label values outside the saved question's support. Dataset output
contains original state and raw answer evidence rather than run envelopes;
the input histories remain in the source file and are not rewritten.

Example input fixture: `{"id":"case-1","state":"Text","label":"true"}`.
Collect it with `judge --questions predicate.json --state-field state`, then
convert with `eval dataset --answer relevant`. Noul labels are strings
`true`/`false`, Choice labels option keys, Score labels level-index strings.
Use package validation for splits, digests, cohort compatibility, metric
definitions, no-fit outcomes, and frozen comparisons. No silent refitting.

### Executor and failure boundaries

The internal `App` accepts injected client construction, stdin, stdout,
stderr, and context for deterministic tests. `RunLive` reads finite chunks,
builds one request per record, and uses bounded workers. Several questions
for one state share the SDK request. Distinct records never share state or
conditional intermediate answers. Finish/write one chunk in input order
before reading the next; memory is bounded by chunk and record limits.

`Envelope` owns raw data, runs, and extra fields. `AppendRun` enforces unique
names. `SelectedRun` resolves latest/name without success fallback.
`Run.Request` and `Run.ValidatedResponse` reconstruct saved evidence and
enforce request/answer validation, including full key sets and saved errors.
Workflow helpers must use these functions rather than decode answers alone.

Per-record request failures produce error-bearing output and processing
continues within bounded input, ending with exit 2. A malformed line stops
input at that line, reports its position, and does not invent a record;
already completed chunks may have been emitted. No all-or-nothing output
promise. Cancellation stops new work and exits 130 under the main signal
contract, including an already-canceled context.
SDK retries remain SDK behavior; CLI does not restart a completed stream.
Write failure or closed pipe cancels further work immediately and exits 2.

All commands support help. Success is 0, defined semantic rejection is 1,
usage/input/execution failure is 2, cancellation is 130. Offline gate emits review/escalation
decisions with exit 0 unless execution fails; check is the CI exit adapter.
Empty rank/pack streams emit nothing and succeed; empty eval datasets fail.
No retry defaults, thresholds, probabilities, or confidence are rewritten.

## Alternatives considered

Ten independent binaries have familiar invocation names but duplicate
packaging and record contracts. One generic `judge` plus shell recipes has
less command surface but leaves candidate mapping, abstention, ordering,
and validated policy use to each user. One executable with shared execution
and focused adapters preserves the useful boundaries of both approaches.

Implicit matching and inferred token budgets offer shorter commands but
hide two unowned decisions: candidate search/assignment and tokenization.
Supplied pairs and explicit bytes make their limits inspectable.

## Tradeoffs and consequences

Histories and saved questions increase record size. The limits make failure
visible rather than silently dropping provenance. Collection operations
retain a bounded dataset in memory; live commands process finite chunks.
Output can be partial before an operational failure, so shell users should
check exits before accepting an artifact as complete.

The CLI depends on experimental packages and exposes their current
semantics, including ranking and compaction tie behavior. Pin a module
version for reproducibility. This design preserves observations but makes
no claim about model accuracy, costs, latency, or downstream task quality.

## Rollout

Review the PRD/spec/decision before release. Ship the executable through
the existing module install path; no root-package migration is required.
Document installation, all commands, a fake-tested composition transcript,
exit codes, and experimental boundaries. Existing examples remain live and
minimal under decision 0002; test fixtures belong in CLI tests.

Validation covers each command, saved evidence tampering, worker ordering,
limits, error distinctions, byte accounting, and calibration digests.
Run the PRD checker, `gofmt`, `go vet ./...`, and `go test ./...`. A complete
local build and fake-server proof do not imply a published release or live
model qualification. Obtain independent implementation review locally.

## Open questions

None blocks this version. Candidate finding, additional input formats,
token-budget adapters, and global matching need separate scope and evidence.
