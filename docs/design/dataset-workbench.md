# Dataset CLI and workbench

Status: Approved
Workflow: Implement the approved proposal, independently review, then open one PR.

## Boundaries

`internal/dataset` normalizes local, stdin, and HTTP inputs into streaming items.
`internal/catalog` loads declarative skills, parameters, examples, and patterns.
`internal/jobs` prepares requests and manages durable execution and composition.
`internal/workbench` presents these operations with Wonton.
`internal/cli` binds commands, options, JSON output, and diagnostics.
The root library never imports these packages and keeps its existing API.

Sources supply JSON values, whole text files, lines, or embedded image assets.
Selection accepts repeatable include/exclude globs, ignore-file rules, explicit
JSON pointers for expansion and state, source manifests, limits, and seeded samples.
No JSON array is expanded automatically. Local paths are relative to source roots.
Reader and HTTP operations have record/source limits and context cancellation.
Images use the existing Cloudflare helper; unsupported inputs fail before model work.

## Skills and patterns

Bundles contain `skill.json`, `SKILL.md`, and optional examples. The manifest
declares name, description, input types, typed parameters, native questions,
state mapping, and optional policy. Builtins, project `.decide/skills`, and
personal `DECIDE_HOME/skills` are explicit library scopes. Short names work when
unambiguous. Parameters interpolate only declared placeholders in JSON values.

Pattern configurations compose named skills. Mapping is the default; heads
uses independent selector/branch questions; funnel carries stage results and
screening decisions; gate applies explicit rules; rank and rank-pack operate
on saved evidence. Configuration validation rejects unknown stages and cycles.

## Runs

Each run has an ID and directory beneath `.decide/runs` or `DECIDE_RUNS_DIR`.
The run freezes skill/pattern configuration, provider/model settings, and source
selection. Prepared inputs are spooled before submission; image assets use
content digests. Append-only attempt and result records permit recovery after
an interruption. Persisted successful results are not resubmitted. A submitted
request with no recorded response is uncertain, not automatically safe to retry.
Credentials are resolved at execution and never saved in artifacts.

Concurrency, prefetch, rate, byte limits, timeout, retries, and request ceilings
bound resource use. Counts include retries and composition stages. Results are
written as they complete by default; ordered output remains available and bounded.
Collection patterns have explicit collection limits. Limits and partial completion
are visible in the run summary. Run artifacts retain source identity and request
evidence. Existing saved envelopes keep their discriminator and remain readable.

## Interaction

`explore` and `inspect` enter a Wonton full-screen interface. The workbench
shares dataset, catalog, and jobs operations with batch commands. Background
operations post events; only the event loop mutates display state. The source
picker, skill browser, prepared-state preview, sample execution, progress, result
detail, comparison, and export form one workflow. Network or file reads never
block the event loop. Exiting cancels work and leaves its artifact resumable.

Batch commands never launch a screen or ask a question. Help is grouped around
sources, judgments, execution, and evidence. Friendly copy lives in interactive
guidance and success summaries; error messages stay specific and factual.

## Tradeoffs and rollout

Durability costs disk space. Inputs are spooled as consumed instead of loading
entire datasets. Large images are stored once per digest. Collection operations
retain explicit bounds. Wonton adds CLI dependencies without changing root imports.
Existing primitive commands remain compatible. Source preparation, durable jobs,
catalog management, and workbench share one implementation and ship together.

Fake HTTP providers, interrupted-run recovery, source selection, image capabilities,
and Wonton event/render tests provide offline qualification. PTY acceptance checks
verify terminal startup, key routing, and cleanup. No paid live calls are required.
