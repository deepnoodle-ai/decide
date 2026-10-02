# Examples

Run commands from the repository root with Go 1.27 or later.
Each program uses fixed input so you can read the whole pattern in `main.go`.

Start with the root package's executable examples for
[`Eval` and `Pick`](../eval_example_test.go) or
[typed request handles, validation errors, and answer helpers](../example_test.go):

```sh
go test -run '^Example' ./...
```

These tests use fake answers and run without credentials. Their output checks
API behavior, not model accuracy.

## Command examples

These programs demonstrate the experimental `x/` packages.

| Program | Pattern | API requests with the supplied input, before retries |
| --- | --- | --- |
| [pick](pick/main.go) | Extract email candidates, select an original address, or abstain. | 1 |
| [gate](gate/main.go) | Apply different confidence thresholds to read and delete proposals. | 1 |
| [fanout](fanout/main.go) | Judge three passages concurrently and retain per-item failures. | 3 |
| [rank](rank/main.go) | Judge three passages and sort them by relevance. | 3 |
| [calibrate](calibrate/main.go) | Fit a cutoff on labeled fixtures and report held-out errors. | 0 (offline) |
| [heads](heads/main.go) | Ask a selector and conditional follow-ups together. | 1 |
| [funnel](funnel/main.go) | Screen tool summaries, then check up to two full descriptions. | 1–3 |
| [compact](compact/main.go) | Keep whole transcript segments or supplied short forms under a byte budget. | Batched; prints the count |

Run calibration without an API key:

```sh
go run ./examples/calibrate
```

```text
flag urgent when noul >= 0.90
held-out: 2 of 3 flagged, 1 wrong
```

The probabilities and labels are invented. The held-out error illustrates why
zero fit errors do not guarantee future accuracy. Use independently labeled
answers from one resolved model version for a real calibration dataset.

For the other programs, set `TYPESAFE_API_KEY` in your environment, then run
one program, for example:

```sh
go run ./examples/pick
```

These programs send their fixed inputs to the configured API. Live probabilities
and selections can vary. Thresholds are illustrative; measure them on your own
data. The programs print judgments and decisions without executing file
operations, sending email, or invoking the selected tools.

A pick selects from supplied candidates; it does not generate a new value.
Heads asks every branch in advance; follow-ups cannot see the selector's answer
in the same request. See the [heads note](heads/README.md) for when to use a
second request. Compact uses bytes for clarity; use tokenizer counts when your
application's budget is in tokens.
