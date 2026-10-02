# Decision tool package boundaries

Status: Accepted
Date: 2026-10-01
Workflow: Spec, independent review, then implementation.

## Proposal

Use module `github.com/deepnoodle-ai/decide`, package `decide`, and fake-server
package `decidetest`. Replace package references, imports, error and logging
prefixes, SDK tokens, contributor guidance, and examples together. Keep the
provider protocol, default endpoints, environment keys, and version intact.
Rename the GitHub repository to match; retain its current visibility.

The root remains a standard-library-only client. Application policies stay
in experimental packages. No root package imports `x/` or the CLI.

Add capabilities in this order, with each slice building on the prior slice:

| Slice | API and responsibility | Dependencies |
| --- | --- | --- |
| Root API | Module name, typed Eval, candidate Pick, fake server | Existing client |
| Gate | Explicit rules, three outcomes, explained `Decision` | Root |
| Fanout | Bounded independent requests and ordered results | Root |
| Rank | Candidate ordering, pairwise sorting, budgeted prefix | Root |
| Calibrate | Offline threshold fit and held-out comparison | Root, gate |
| Heads | Selector and speculative branch questions | Root |
| Funnel | Staged screening, retained answers and reports | Root, fanout |
| Compact | Scored selection of verbatim segments under budget | Root, fanout |
| CLI | Eleven composable commands and saved JSONL evidence | Root, experimental packages |

## Root convenience API

```go
func Eval[A Answer](
    ctx context.Context, client *Client, state any,
    question QuestionFor[A], opts ...RequestOption,
) (Evaluation[A], error)

type Evaluation[A Answer] struct {
    Answer A
    Response *Response
}

func Pick[T any](
    ctx context.Context, client *Client, state any, instructions any,
    candidates []Candidate[T], opts ...RequestOption,
) (Decision[T], error)

type Candidate[T any] struct {
    Item T
    Description any
}

type Decision[T any] struct {
    Item T
    Index int
    Picked bool
    Answer *ChoiceAnswer
    Response *Response
}
```

`Eval` asks one question under the fixed key `eval`. Type inference follows
`QuestionFor[A]`, including external implementations. An internal adapter
uses `NewAnswer` for decoding and forwards an optional `AnswerValidator`,
so an external question needs no global answer registration. Before wrapping,
`Eval` validates a request containing the original question. This preserves
`Request.Validate` checks for concrete built-in question types. An empty
Choice and a Score with fewer than two levels must fail before network work;
a custom `QuestionFor` must still decode without global registration.
`Evaluation.Answer`
is the original typed answer. `Response` preserves model, usage, request ID,
provider headers, and validation diagnostics. Batch callers keep using
`NewRequest`, `Ask`, and `Client.SystemOne`.

`Pick` builds one Choice under key `pick`. Candidate keys are positional
(`c1`, `c2`, ...); descriptions are caller-supplied JSON values. The extra
option `none` means abstention. Choice evidence exposes exact probabilities
and confidence. A selected result has `Picked == true` and its source index;
abstention has `Picked == false`, `Index == -1`, and the zero item. An empty
candidate list abstains locally with no fabricated answer or response.
The fixed cap is 254 candidates plus abstention. Request options select a
model or add provider inputs, including embedded images.

Both functions honor context cancellation before network work and reject
nil context/client. They perform request and answer validation even when
client-wide answer validation is disabled. Request, transport, and structural
answer failures produce no selected item or typed answer. Failures retain any
response returned by `Client.SystemOne`; the client discards responses on
transport errors. Consistency-only errors are the exception: they return the
full answer or selection with an error matching `ErrInconsistentAnswer`, so
`Decision.Picked` can be true alongside that error. Every validation failure in the
response must be a consistency failure; a mixed structural/consistency
error produces no selected item or typed answer. No error becomes a successful abstention. A canceled empty pick returns the
context error, not a success.

`Pick` remains small and uses the root Choice and handle machinery. The
existing experimental picker keeps its configurable keys, reusable question
builders, and multi-question batching API. The root never imports it.
Neither root operation applies thresholds or returns Allow/Review/Escalate.
Those explicit policies remain in `x/gate` and return `gate.Decision`.

The CLI becomes `decide`; its tour override becomes `DECIDE_CLI`. Keep
`typesafe_cli: 1` on disk so previously saved observations remain readable.
User guides must distinguish network commands from offline operations.

## Alternatives and consequences

A single generic result for judgments and candidate selections would add
meaningless selection fields to Noul and Score results. `Evaluation[A]`
retains typed judgments; `Decision[T]` records a selected item or abstention.
Moving action policies into the root would widen its compatibility contract.
Keep them in `x/gate`, with no default thresholds.

One large capability PR would be simpler to publish but harder to review. Each
slice gets a buildable commit and stacked PR. The stack must merge in order
and may need rebasing after squash merges.

## Verification

Run `gofmt`, `go build ./...`, `go vet ./...`, and
`go test -race -count=1 ./...`. Verify `go mod tidy` leaves module files
unchanged. Compile live-tag tests without making live requests. Check
imports and execute fake-backed examples. Obtain independent implementation
review before opening each PR. Use fake-backed examples as evidence.
