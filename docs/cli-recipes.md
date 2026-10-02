# decide: eleven tools, four small adventures

Run the [live tour](../scripts/cli-tour.sh) once. It needs Bash, jq, the
installed CLI, and `TYPESAFE_API_KEY`. It prints the temporary directory
containing its inputs and real observations. `cd` there to try these
variations. Use Bash's `set -o pipefail` so a failed CLI command makes its
pipeline fail too. To use a binary outside PATH:

```sh
DECIDE_CLI=/absolute/path/to/decide bash scripts/cli-tour.sh
```

The tiny corpus demonstrates composition. Its cutoffs and four labeled
cases do not establish useful calibration or accuracy on your own data.
Saved observations include the source content; keep them accordingly.

## Rescue notes, hold the banana cake

`questions.json` asks whether a passage explains session recovery. The
tour's `judge --as rescue` saves that answer once per passage. Then:

```sh
decide rank --run rescue --answer relevant < observed.jsonl \
  | decide pack --run rescue --answer relevant --budget-bytes 240 \
  | jq -r 'select(.runs[-1].result.selected) | .data'
```

Rank puts higher probabilities first. Pack chooses whole strings under
240 UTF-8 bytes. Neither calls the model. Banana cake may be good; it is
poor advice for restoring a session.

For fresh filtering, ask with `grep`. For reusable policy decisions, use
`gate` on the saved evidence:

```sh
decide grep --input text --threshold 0.8 \
  --question 'Does this passage explain restoring an interrupted session?' \
  < passages.txt
decide gate --run rescue --policy policy.json < observed.jsonl \
  | jq '{id, outcome: .runs[-1].result.outcome}'
```

`policy.json` uses bands: allow at 0.8+, review at 0.5+, escalate below.
Those are demo choices. Gate reports them without taking action. `check`
makes fresh judgments with the same policy and exits 1 when any record
needs review or escalation:

```sh
decide check --questions questions.json --policy policy.json \
  < passages.jsonl > checked-again.jsonl
```

Read the status immediately (`echo "$?"`). An operation failure is exit 2.

## Two tickets and a wish

`teams.json` describes billing and engineering. `rubric.json` describes
three levels of proposal specificity, from a vague wish to defined behavior.

```sh
decide label --taxonomy teams.json < tickets.jsonl \
  | jq '{id, team: .runs[-1].result.label}'
decide score --rubric rubric.json < proposals.jsonl \
  | jq '{proposal: .data, judgment: .runs[-1].response.answers.specificity}'
```

Edit the criteria, then rerun. Labels are your option keys. Score levels
are ordered descriptions; the answer includes the full distribution.

## Receipts and suspiciously familiar suppliers

The receipt sender supplies two addresses. Your code enumerates them;
`pick` chooses one or abstains. `join` gets already prepared supplier pairs.

```sh
decide pick --question 'Which address should receive the receipt?' \
  < candidates.jsonl | jq '.runs[-1].result'
decide join --question 'Do left and right describe the same supplier?' \
  < pairs.jsonl | jq '{id, p: .runs[-1].response.answers.relation.noul}'
```

Pick returns the original item, not generated contact details. Join returns
a probability per supplied pair, not a unique supplier assignment.

## Did it get worse?

The tour saves ground truth outside model state, converts observations into
fit and held-out datasets, fits an artifact, and compares that artifact to
its original held-out observations. That self-comparison is a wiring check.

To evaluate another model on the *same* held-out cases, set
`CANDIDATE_MODEL` to its actual model ID and run in Bash with `pipefail`:

```sh
set -o pipefail
jq -c 'select(.id | startswith("heldout-"))' cases.jsonl \
  | decide judge --questions questions.json --state-field state \
      --model "$CANDIDATE_MODEL" \
  > candidate-observed.jsonl
decide eval dataset --answer relevant < candidate-observed.jsonl \
  > candidate.json
decide eval compare --artifact artifact.json --candidate candidate.json \
  --tolerance tolerance.json > candidate-comparison.json
```

Compare stays offline and does not refit. It rejects mismatched questions
or held-out cases; exit 1 means the measured change exceeds your tolerance.
The demo allows a raw Brier score increase of 0.01. Use a representative
labeled corpus and choose a meaningful tolerance for real evaluations.
See [patterns/calibrate](../patterns/calibrate) for configuration and metrics.

Every emitted live run records the resolved model, request ID, and usage.
Inspect those alongside the answer rather than assuming a moving model
alias still points to the same version.
