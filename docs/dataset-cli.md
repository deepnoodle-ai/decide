# A judgment notebook for your datasets

Choose some sources, try a small sample, and keep the evidence. Decide's dataset
commands apply reusable judgments to files, directory trees, JSONL, JSON exports,
images, public HTTP sources, and stdin. The Wonton workbench helps you experiment;
batch commands never prompt. These commands, bundle schemas, and artifacts are
experimental.

## Start with a taste

```sh
go install ./cmd/decide
decide skills list
decide sources list . --include '**/*.go' --exclude '**/*_test.go'
decide plan builtin/code-risk . --include '**/*.go' --sample 5 --seed 42
decide explore . --skill builtin/code-risk --include '**/*.go'
```

Sources, library browsing, and `plan` need no model credentials. Plan prints
prepared-item JSONL and a stderr count. Public HTTP sources still perform HTTP
reads. Random sampling scans the selected sources; `--limit 5` stops after the
first five items instead.

In the workbench, choose a skill, preview with `p`, and send that exact sample
with `s`. Edit parameters or questions and run the sample again to compare.
Full-dataset work is a separate `r` action. All model work retains its run artifact.
Export with `e` to a new directory: it contains evidence, configuration, an edited
skill or frozen pattern bundle, and a runnable batch command. Recorded models,
endpoints, and questions survive changes to your catalog or environment. The
command reads the original sources again; retain the run to inspect its original
input snapshot. Frozen exported skills use `resolved: true` and accept no new
parameters. The workbench uses terminal
stdin for keys; choose files or URLs instead of `-` there.

Use `a` to add a source, `i`/`x` for include/exclude globs, and `t` for a
`NAME=VALUE` parameter. Arrow keys navigate; `/` filters evidence; `?` shows
the field guide. Advanced editors expose source, execution, and question JSON.
The notebook keeps bounded evidence windows while full results remain on disk.

## One source or all these sources

```sh
decide run builtin/code-risk ./src/server.go
decide run builtin/code-risk ./src ./internal --include '**/*.go' \
  --exclude '**/*_test.go' --param focus=authorization
decide run builtin/ticket-routing ./tickets --include '**/*.json'
decide run builtin/ticket-routing ./tickets.jsonl --workers 8
cat tickets.jsonl | decide run builtin/ticket-routing -
decide run builtin/receipt-quality ./scans \
  --include '**/*.{png,jpg,jpeg,webp}' --provider cloudflare
```

| Source | Default judgment unit |
| --- | --- |
| Code or text file | Entire file |
| JSON file | Entire JSON value |
| JSONL file | Each nonblank line |
| Image file | One image with source metadata |
| Directory | Each matching file, interpreted individually |
| HTTP URL | Response interpreted by content type or explicit format |
| `-`, or omitted sources with piped input | JSONL records from stdin |

Quote include/exclude globs so Decide evaluates them relative to each source
root. Globs support `**` and braces. Exclusions win. Directory traversal honors
ancestor and nested `.gitignore` and `.ignore` rules. Symlinks are skipped unless
`--follow-symlinks` is supplied; cycles are rejected or skipped. `.git` is not a
dataset. Use `--no-ignore` to override ignore rules explicitly.

`--format auto|json|jsonl|text|lines|image` overrides interpretation. `text` means
one whole file; `lines` means individual lines. A JSON array remains one item
until explicitly expanded:

```sh
decide sources preview records.json --items ''
decide plan builtin/ticket-routing export.json --items /tickets \
  --state /description --id-field /id
decide sources preview 'https://example.com/export.json?page=1' --items /tickets
```

Pointers use JSON Pointer syntax, including `~0` and `~1` escapes. State selection
preserves original `data` separately. Explicit IDs are qualified by source identity
so independent files do not collide. Provenance includes URI, relative display
path, format, digest, and line/index location. Text-code skills also prepare
path, language, and content in model state.

Whole-document JSON, text, and image inputs are bounded by `--max-item-bytes`
(default 16 MiB). Each source is bounded by `--max-source-bytes` (default 1 GiB).
JSONL and lines stream within these bounds. JSON array expansion currently reads
the bounded JSON document; use JSONL for very large datasets. Source discovery
and sampling use bounded memory and temporary disk storage. Local digests require
an additional streaming read of the source file.

A manifest gives different sources their own mappings. Relative paths resolve
against the manifest's directory:

```json
{
  "version": 1,
  "sources": [
    {"path": "./tickets", "include": ["**/*.json"], "state": "/description"},
    {"url": "https://example.com/export.json", "items": "/tickets", "state": "/description"}
  ]
}
```

```sh
decide plan builtin/ticket-routing --sources sources.json
```

URLs support public query parameters. Userinfo, fragments, and recognized credential
or signing query parameters are rejected without echoing their values. There is
no implicit crawling, pagination, or remote skill execution.

## Make a skill your own

A skill defines the judgment; a pattern defines its composition; a run records
one execution. A skill bundle contains `skill.json` and `SKILL.md`. Example data
and questions are embedded in the manifest, keeping the bundle portable.

```sh
decide skills new project/team-routing --from builtin/ticket-routing
decide skills edit project/team-routing
decide skills validate project/team-routing
decide skills test project/team-routing
decide skills show project/team-routing --json
```

Edit uses `VISUAL`, then `EDITOR`. You can also edit the displayed path directly.
Builtin skills are read-only; copy them first. `project/` lives in `.decide/skills`;
`user/` lives in `DECIDE_HOME/skills`. Bare names work only when unambiguous.
Explicit bundle directories and manifest paths work too.

```json
{
  "version": 1,
  "name": "team-routing",
  "description": "Find the right team for each ticket.",
  "inputs": ["json", "jsonl", "text", "lines"],
  "state": "value",
  "parameters": {
    "team": {"type": "string", "default": "billing", "description": "Team to assess"}
  },
  "questions": {
    "fits": {"type": "noul", "instructions": "Does this ticket belong to {{team}}?"}
  },
  "examples": [{"description": "I was charged twice."}]
}
```

Parameters have string, number, integer, boolean, or json types. Repeat
`--param NAME=VALUE`. Placeholders substitute declared values within JSON values,
without executing code. `state: "file"` prepares path/language/content;
`state: "value"` uses the selected source value. An optional native `x/gate`
policy retains its decision. The `pattern` field is descriptive guidance;
choose an executable pattern with `--pattern`.

Skill tests are offline definition/example checks. `skills test NAME --live`
explicitly sends examples to a provider and saves evidence. These checks do not
prove model quality; labeled calibration is still needed for real routing.

## Compose judgments

```sh
decide patterns list
decide patterns show builtin/funnel --json > review.json
# Edit the questions, models, and cutoffs for your task before using them.
decide run --pattern review.json . --include '**/*.go'
```

Pattern manifests use version 1 and a `type`:

| Type | Fields and behavior |
| --- | --- |
| `map` | `skill`: independent judgment per item |
| `heads` | Choice `selector`, `branches` mapping every option to a skill; one request |
| `funnel` | Ordered `stages`: name, skill, optional model, answer/threshold, limit, policy |
| `gate` | `run` source ID/path and explicit `policy`; offline saved evidence |
| `rank` | `run`, Noul `answer`, optional `max_records`; offline sorting |
| `rank-pack` | Rank fields plus positive `budget_bytes`; select whole strings |

For saved-evidence patterns, set `run` to an existing run, exported JSONL, or old
`typesafe_cli: 1` evidence file. Those operations use no model credentials.
Collection patterns have explicit item bounds (`--max-records`, default 10,000)
and byte bounds (`--max-collection-bytes`, default 64 MiB).
Funnel stage limits rank survivors globally, also requiring bounded collection.
Dropped items remain exported outcomes, distinct from failures.

Heads asks selector and branches together. Branch questions must be independently
answerable from the same original state; a branch cannot read its selector's answer.
Use a later funnel stage for dependent work. Example thresholds are illustrative,
not recommended cutoffs. Measure policies with the existing `eval` commands.

## Keep a run you can reopen

`run` prepares and saves the entire selected input snapshot before sending model
requests. It then executes with bounded workers and writes per-item evidence to
`.decide/runs/ID`, or `DECIDE_RUNS_DIR` / `--run-dir`.

```sh
decide run builtin/relevance passages.jsonl --workers 8 \
  --request-timeout 30s --retries 2 --max-requests 5000
decide runs list
decide runs show RUN_ID --json
decide runs watch RUN_ID --json
decide inspect RUN_ID
decide runs export RUN_ID > results.jsonl
decide runs resume RUN_ID
decide runs resume RUN_ID --max-requests 10000
```

Every provider attempt, including retries and stages, counts against the request
ceiling. Only retryable HTTP responses are retried automatically. A submitted call
without a recorded outcome is uncertain and requires `--retry-uncertain`; a timeout
does not prove that the provider performed no work. Known failed items require
`--retry-failed`. Successful item and stage evidence is retained across resume.
An explicit resume ceiling increase permits additional attempts; questions, models,
and input selection stay frozen.

Interrupted preparation is marked `preparation-incomplete` and cannot execute-resume:
restart it from the original sources. This is essential for unread stdin. Once
prepared, stdin and remote snapshots are durable. `--snapshot refs` verifies local
source digests on resume; `--snapshot copy` uses the retained inputs independently.
Both retain consumed inputs on disk. Images are assets addressed by digest instead
of repeated base64 payloads in every output record.

`--order completion` is the dataset default. `--order input` exports persisted
results in input order after execution, keeping the reorder buffer off the heap.
`--rate-limit` bounds attempt admission per second; `--on-error stop` cancels further
work after failure. `--output FILE` requires a new file and refuses to overwrite an
existing source or export. Stdout contains result JSONL; stderr contains summaries.
Use `--progress none` to suppress routine summaries.
Partial runs and limit stops are visible in status and exit 2. Cancellation is 130.

## Choose a connection

TypeSafe uses `TYPESAFE_API_KEY`. Cloudflare uses `CLOUDFLARE_AUTH_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID`. The default models are `jev-latest` and `clef` respectively.
Cloudflare supports embedded PNG/JPEG/WebP through its existing image helper.
Image capability and request-size validation happen before model dispatch.

Use `--provider`, `--model`, `--base-url`, and `--account-id` for explicit settings.
`DECIDE_PROVIDER`, `DECIDE_MODEL`, and `DECIDE_PROFILE` provide defaults.
Profiles live in `DECIDE_CONFIG` or `DECIDE_HOME/settings.json`:

```json
{
  "profiles": {
    "vision": {"provider": "cloudflare", "model": "clef-flash", "account_id": "YOUR_ACCOUNT_ID"}
  }
}
```

Flags override environment defaults, which override profiles. TypeSafe compatibility
defaults (`TYPESAFE_DEFAULT_MODEL`, `TYPESAFE_BASE_URL`) apply only to TypeSafe.
Credentials are resolved when executing and are never configuration fields or CLI
flags. `DECIDE_HOME` defaults to `XDG_CONFIG_HOME/decide` or `~/.config/decide`.

The original eleven pipeline commands and their `typesafe_cli: 1` envelopes remain
supported. `decide inspect old.jsonl` revalidates saved evidence. See [CLI pipelines](cli.md)
and [recipes](cli-recipes.md) for one-off judgments and offline calibration.
