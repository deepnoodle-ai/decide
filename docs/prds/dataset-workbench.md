# Apply reusable judgments to datasets

Status: Approved
Implementation PR: [#18](https://github.com/deepnoodle-ai/decide/pull/18)
Updated: 2026-10-01

## Problem and context

Developers use decision models on code trees, JSON exports, and image collections.
The existing CLI accepts JSONL and text lines. Users must prepare those inputs,
write questions, manage evidence files, and recover interrupted work themselves.
The approved direction adds source selection, reusable skills and patterns,
durable runs, and an explicit Wonton workbench. Existing pipelines remain usable.

## Concepts

- A source supplies items: files, directory trees, JSONL, JSON arrays, HTTP responses, or stdin.
- A skill defines typed questions, input preparation, parameters, and examples.
- A pattern composes judgments through mapping, heads, screening, policy, or ranking.
- A run records one execution, its resolved configuration, sources, attempts, and results.

## Use cases and acceptance

### Select a code tree

`decide plan code-risk . --include '**/*.go' --exclude '**/*_test.go'`
previews the selected items and prepared state without model calls.

- Directory selection is deterministic; exclusions win; ignore files apply by default.
- A text file is one item; JSONL supplies one item per nonblank line.
- JSON array expansion requires an explicit JSON pointer.
- Every item retains source identity, path or URL, content digest, and location.
- Source lists, previews, limits, and deterministic samples work without model credentials.
- Remote reads and records have explicit byte limits; execution uses bounded memory.

### Experiment and execute

`decide explore . --skill code-risk --include '**/*.go'` opens a workbench.
`decide run code-risk . --include '**/*.go'` runs the same task without prompts.

- Shipped skills include code assessment, ticket routing, and image assessment.
- Personal and project skill bundles are supported; ambiguous names are rejected.
- Skills have documentation, typed parameters, validation, examples, and offline tests.
- Mapping, heads, funnel, gate, rank, and rank-pack have inspectable pattern examples.
- Configured compositions retain stage results and distinguish drops from failures.
- Cloudflare images are validated before submission and are retained as run assets.
- Provider selection does not leak TypeSafe environment defaults into Cloudflare.
- Batch commands never prompt; stdout is structured and stderr carries diagnostics.

### Recover and inspect

`decide runs resume ID` uses recorded inputs and configuration.
`decide inspect ID` opens a filterable evidence browser.

- Runs preserve prepared inputs, recorded configuration, attempts, results, and completion state.
- A stopped or canceled run can resume without repeating successful results.
- Failed and uncertain attempts remain distinct; retrying uncertain work is explicit.
- Run listings, summaries, watching, and export also support scripts.
- The workbench shows source preview, skill documentation, progress, results, and errors.
- Keyboard controls are discoverable; empty and error states explain the next action.
- The workbench can run a sample, inspect answers, compare experiments, and export evidence.
- Existing `typesafe_cli: 1` files remain readable and inspectable.

## Decisions

- Use explicit interactive commands. Batch mode must remain safe for pipelines and CI.
- Keep skills declarative. Loading a library item does not execute arbitrary code.
- Record consumed inputs on disk. Resume must not depend on reconstructing stdin.
- Keep policies in experimental application code; the root client remains standard-library-only.
- Deliver one PR as requested. Requirements and design accompany implementation.

## Bounds

Remote registries, implicit crawling or pagination, video input, arbitrary workflow
programs, and dollar budgets are outside this proposal. File-level assessment is
not represented as whole-application architectural evidence. User-supplied
thresholds and source transformations remain explicit.
