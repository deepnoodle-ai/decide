# Decision tools in Go and shell pipelines

> Historical record of the original implementation plan. Package promotion
> supersedes its experimental-package layout and graduation exclusions.
> The old `x/` paths below are historical, not current imports. Use
> `backend`, `cloudflare`, and `patterns/<name>` instead. See the
> [README](../../README.md) for current APIs and the pre-v1 policy.

Status: Approved
Updated: 2026-10-01

## Problem

Developers need typed model judgments and explicit policies in Go and shell
pipelines. The library has a client and candidate picker, but lacks
gating, ranking, calibration, staged screening, and pipeline commands.
The selected module name is `github.com/deepnoodle-ai/decide`.

## Users and job

Go developers ask typed questions, retain the returned evidence, and apply
their own action policies. Shell users save the same evidence and reuse it
in offline commands.

## Context

The root client, fake HTTP server, and `x/pick` exist. `x/backend` and
`x/cloudflare` select and adapt TypeSafe Jev and Cloudflare Clef. The additional tools must retain explicit policies, per-item failures,
and reusable evidence.

## Use cases and acceptance

- Call `decide.Eval` with one typed question and obtain its typed answer
  and response evidence. Call `decide.Pick` with described candidates and
  obtain the original item or an explicit abstention with choice evidence.
  Neither function chooses an application threshold or executes an action.

- Import `decide` and `decidetest`. Construct the same requests, typed
  handles, and fake-server fixtures. Existing answer validation, retries,
  environment settings, and backend adapters retain their behavior.
- Apply `x/gate` rules to saved answers and obtain a `Decision` with an
  outcome and explanation. Missing inputs retain explicit fallback rules.
- Run `x/fanout` with bounded concurrency. Results retain input order and
  individual errors; cancellation stops further work.
- Order and select candidates with `x/rank`, including pairwise inputs.
- Fit thresholds and compare held-out labeled answers with `x/calibrate`.
  These operations make no network calls.
- Build a selector and branch questions with `x/heads`; read only the
  selected branch's answers.
- Screen items through `x/funnel` stages with retained answers, drop
  reasons, per-stage reports, and individual failures.
- Score context segments with `x/compact` and retain whole source segments
  or caller-supplied short forms under an explicit budget.
- Add `cmd/decide` with eleven commands: `judge` asks named typed questions;
  `grep` filters by an explicit Noul threshold; `label` selects a taxonomy
  option; `score` applies ordered rubrics; `pick` selects an original candidate
  or abstains; `join` judges supplied pairs; `rank` orders saved answers;
  `pack` selects whole strings within a byte budget; `gate` applies a saved
  policy; `check` asks questions and sets a policy-based exit status; and
  `eval` converts, fits, and compares labeled evidence.
- The CLI reads JSONL by default and supports text lines for live commands.
  Output records preserve original data and append named runs containing
  actual request state, questions, response evidence, and individual errors.
  Stdout carries results or help; stderr carries diagnostics. `rank`, `pack`,
  `gate`, and all `eval` operations work without constructing a model client.
- CLI cancellation stops network work and returns exit 130. Exit 0 means
  completion; exit 1 means no grep match, a rejected check, or an evaluation
  regression; exit 2 means usage, input, evaluation, or output failure.
  Partial output is permitted on failure. Live commands default to ordered
  32-record chunks, four workers, and a 1 MiB record limit. Collecting commands
  default to 10,000 records or 64 MiB, with configurable limits.
- Every package has package comments and a runnable example. Fake-backed
  examples and tests execute without provider credentials.

## Decisions

- The package is the verb `decide`; `Decision` remains a result type.
- Add root-level `Pick` and `Eval` convenience functions. Keep policies,
  collection workflows, and calibration under `x/`.
- Preserve provider wire fields, probabilities, and `TYPESAFE_*` settings.
  Provider configuration is independent of the library's name.
- Preserve the CLI's `typesafe_cli: 1` saved-envelope discriminator.
- Add user guides for each operation. Keep research and process notes
  outside the repository.

## Success

All eight experimental decision packages, backend adapters, fake server,
examples, and CLI build together. Behavioral tests cover the
root convenience calls and every experimental workflow. Formatting, build, vet, race tests, and module tidiness pass
for each independently reviewable slice.

## Not in scope

New model providers, vanity imports, graduation of existing experimental
packages, release publication, visibility changes, and PR merges.
