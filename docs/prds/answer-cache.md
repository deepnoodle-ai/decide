# Pay once for each answer

Status: Draft PRD PR: none Implementation PR: none Updated: 2026-10-04

## Problem

decide sends one request for each item, every time it runs. When nothing has
changed, it asks again and the person pays again. This blocks three uses
that the roadmap and the plugin point at:

- **Sweeps of a whole codebase.** A repository of 100,000 functions is
  100,000 requests a run. Run it each night for 30 nights and that is 3
  million requests, though only about 0.5% of functions change in a day. The
  work worth paying for is about 115,000 requests.
- **CI on each push.** A pull request with five pushes judges the same
  unchanged functions five times. The plugin, `/decide ship` and CI judge
  them again.
- **The plugin's command check.** Claude runs the same commands many times
  in a session, such as `go test ./...`. Each one waits for a request,
  though the answer can't have changed.

Nothing in decide can skip a request today. `runs resume` skips the items
that one run already answered, but a new run starts from zero.

If we do nothing, sweeps stay a one-time audit that few people pay for
twice, and "fix one, find all" and known-bug checks on each diff are too
costly to run as a habit.

## Context

- **Requests.** `runs.Run.evaluate` sends one request per input: an item, or
  one part of a large item. The request holds the item's text as `state`,
  any images, and every question of the template.
- **Questions are independent.** TypeSafe's docs say each question is
  evaluated "in parallel and in isolation against the same state", and the
  [parallel questions
  cookbook](https://docs.typesafe.ai/cookbooks/parallel_questions.md) found
  "no change in answers" between 13 questions in one request and 13 requests
  of one question. So an answer belongs to one item and one question,
  whatever else the request asked.
- **Jev is close to deterministic.** In the same cookbook, most answers were
  identical across 5 repeats. A stored answer is the answer the model would
  almost always give again.
- **The resolved model.** Each response names the version that answered,
  such as `jev-1.13.0`, even when the request asked for `jev-latest`.
  `runs.Result` already saves it.
- **Saved runs.** Each run is saved under `DECIDE_HOME/runs`. The plugin
  uses its own `DECIDE_HOME`, `~/.decide/agent`.
- **The roadmap** lists "Request cache: an opt-in `--cache` that reuses
  identical requests". This PRD replaces that item.
- **Other tools.** Build tools such as Bazel, Turborepo and Nx cache by a
  hash of the inputs, are on by default, and print what came from the cache.
  People trust them because the key covers every input, and a miss is never
  wrong, only slower.

## Users and job

- **Primary:** an engineer who runs decide over a large codebase again and
  again: each night, in CI, or as they work. The job: "Judge everything, but
  charge me only for what changed."
- **Secondary:** the Claude Code plugin, whose checks repeat the same
  commands and pages within a session and must stay fast.

## Concepts

- **Answer cache.** Answers decide has already received, kept on disk. When
  decide would ask the same question about the same text of the same model
  version, it uses the kept answer and sends no request.
- **From cache.** An answer that came from the answer cache instead of the
  model. The run summary and each result say which answers did. A result can
  mix both: some of its answers from cache, the others asked.

The cache holds answers, not decisions. Flags and `--fail-on` are worked out
from answers on each run, as now. So a changed threshold re-asks nothing.

## Use cases

### UC-1: Rerun a sweep

```sh
decide run code-risk src --each function     # night 1
decide run code-risk src --each function     # night 2, after a day's commits
```

On night 2, decide asks only about functions whose text changed. The summary
says:

```
✓ 41,212 answered  3 flagged  48s
  40,987 from cache · 225 asked
```

Acceptance:
- [ ] A second run over unchanged files sends no requests and reports
      every answer as from cache.
- [ ] A function whose text changed is asked again; the others come from
      cache.
- [ ] Results, flags, `--fail-on`, every `--format`, and the saved run
      are the same whether an answer came from cache or the model.

### UC-2: See what a run will cost before it starts

```sh
decide run code-risk src --each function --dry-run
```

The dry run adds one line before the questions:

```
41,212 items · 40,987 in the cache · 225 to ask
```

Acceptance:
- [ ] `--dry-run` reports how many items the cache already answers, with
      no request sent.
- [ ] `--dry-run --json` is unchanged.

### UC-3: Ask fresh

```sh
decide run code-risk src --each function --no-cache
```

Acceptance:
- [ ] `--no-cache` sends every request, and stores the new answers in the
      cache.
- [ ] `runs resume` keeps the setting of the run it resumes.

### UC-4: Keep the cache in CI

```yaml
- uses: actions/cache@v4
  with:
    path: ~/.decide/cache
    key: decide-${{ github.ref }}-${{ github.sha }}
    restore-keys: decide-${{ github.ref }}-
```

Acceptance:
- [ ] The cache is one folder, `DECIDE_HOME/cache`, that can be saved
      and restored on its own, apart from the runs.
- [ ] `docs/recipes.md` shows this step in the GitHub Actions recipes.

### UC-5: A repeated command in the plugin

Claude runs `go test ./...` for the tenth time in a session. The command
check's answer comes from the plugin's cache, with no wait. The content and
reply checks judge text that changes each time (test output, pages,
replies), so they rarely hit.

Acceptance:
- [ ] The plugin needs no change to use the cache.
- [ ] Several decide processes can read and write one cache at the same
      time, without errors, without damaging it, and without losing each
      other's entries.
- [ ] The cache stays at a few hundred files at most, whatever the number
      of answers, so `actions/cache` saves and restores it quickly.

## Requirements

- R-1: The cache is on by default for `decide run` and `runs resume`.
- R-2: A cached answer is used only when all of these match the new request:
  the provider and its address, the model version, the item's text as sent
  (with any context), its images, and the question as sent (after template
  parameters are filled in).
- R-3: Each answer is kept on its own, by question. When some of a
  template's questions are in the cache, decide asks only the others, in one
  request.
- R-4: decide never uses an answer from a model version other than the one
  the provider resolves now. When a request names an alias such as
  `jev-latest`, decide learns the version from responses:
  - The cache records, for each alias, the version last seen and when.
  - A version seen less than an hour ago is trusted.
  - Otherwise, decide sends the run's first request live, whether or not
    its answers are cached, and uses the version that response names.
  - If a later response names a new version, decide uses that version for
    lookups from then on.
  - A request that names an exact version, such as `jev-1.13.0`, needs
    none of this.
- R-5: Clef answers are not cached in this version. Workers AI names no
  model version, so decide can't see an upgrade, and it is not yet measured
  whether Clef answers questions independently.
- R-6: Only valid, complete answers are kept. Failures, errors and invalid
  answers are never kept.
- R-7: The summary shows how many answers came from cache. Each `--json`
  result has a `cached` list: the keys of the questions whose answers came
  from cache, omitted when none did. `request_id` and `model` describe the
  request this run sent for the result, and are empty when it sent none.
- R-8: The cache keeps no item text, file names or images: only what
  identifies a request (hashes) and its answers. Its files are readable only
  by the user, like saved runs.
- R-9: If the cache can't be read or written, decide warns once and runs as
  if it were off. A broken cache never fails a run.
- R-10: `--no-cache` turns the cache off for reads. Answers it gets are
  still stored.
- R-11: The README, `docs/cli.md`, the recipes, the plugin README and the
  changelog describe the cache. The roadmap's "Request cache" item points
  here.

## Not in scope

- **A cache shared between machines.** A laptop and CI keep their own
  caches. A remote store comes after people use the local one.
- **Eviction and a size limit.** An entry is a hash and a few small answers,
  so 100,000 functions with 10 questions each take tens of megabytes. Delete
  the folder to clear it, as with runs. Revisit if sizes prove larger.
- **`decide cache` commands.** Same reason. One folder and one flag are
  enough for now.
- **A cache in the Go library.** The cache belongs to the CLI. Library users
  control their own requests.
- **Ignoring whitespace in the key.** Reformatting a repository misses the
  cache once. Whitespace can carry meaning, as in Python, and a wrong hit is
  worse than a miss.
- **A time limit on entries.** The model version already decides when an
  answer goes stale.
- **Sending identical requests once while they are in flight.** Identical
  items that come one after another already hit the cache. Coordinating
  workers inside a run saves little. Revisit if runs show many identical
  items.
- **Smaller saved runs.** Each run still saves every input, so 30 nightly
  sweeps keep 30 copies of the code under `DECIDE_HOME/runs`. That is a
  separate cleanup for runs, not part of the cache.

## Decisions

- **On by default.** Rerunning is the common case, and the key covers every
  input that changes an answer, so a hit gives what a new request would.
  Opt-in, as the roadmap had it, would leave the saving to people who
  already know to ask for it. Rejected: an opt-in `--cache`.
- **Key by question, not by request.** Questions are independent, so a
  stored answer stays valid whatever else was asked. Adding a question to a
  template then re-asks only that question. The question's key is part of
  the request, so it is part of the cache key: two templates share an answer
  only when they ask the same question under the same key, and renaming a
  key asks again. Rejected: keying a whole request, which misses whenever
  the question set changes.
- **Key by the resolved version, not the alias.** An upgrade behind
  `jev-latest` must not serve old answers. Rejected: keying the name the
  request used.
- **Keep hashes, not text.** The cache can sit in a CI cache or a backup
  without holding source code. Rejected: storing items, which would let the
  cache double as a viewer. Saved runs already do that.
- **In the CLI, not the root package.** The root package stays a client of
  the API. Rejected: a caching `decide.Client`.
- **Trust a version for an hour, then check with one request.** The plugin's
  repeated command checks stay instant, and a sweep pays one request to
  learn of an upgrade. Rejected: always sending the first request live,
  which adds a round trip to every plugin check.
- **A list of cached keys on each result, not a yes or no.** A partial hit
  is common after a question is added, and the list shows it. Rejected: a
  `cached: true` flag.
- **No Clef caching yet.** See R-5. Rejected: keying Clef by model name,
  which would serve old answers after an upgrade nobody can see.

## Success

- **Cost.** On a repository with a month of history, 30 nightly sweeps cost
  under 5% of the requests that 30 sweeps without a cache cost.
- **Speed.** A rerun over unchanged files finishes in under a tenth of the
  first run's time.
- **The plugin.** A repeated command's check returns in under 50 ms, when
  the version was seen in the last hour.
- **No wrong answers.** No reported case where a cached answer differs from
  what the same model version gave for the same request.

Must not get worse: the time of a first run, and the output of every format.

## Risks and open questions

- **Does Clef answer questions independently, like Jev?** Measure it with 1
  vs 8 questions per request on labeled items. If it does, and Workers AI
  can name a model version, Clef can be cached later. *Doesn't block.*
- **An upgrade within the hour.** A run within an hour of the last one could
  serve answers from the old version after `jev-latest` moves. The window is
  short and the answers were valid an hour ago. `--no-cache` asks fresh.
  *Doesn't block.*
- **A poisoned CI cache.** A cache that says "not flagged" lets a bug
  through a `--fail-on` gate. Restored caches must come only from the same
  branch or the base branch, which `actions/cache` enforces for pull
  requests from forks. The recipe uses `github.ref` in the key. *Doesn't
  block.*
- **Close to deterministic is not deterministic.** An answer near a flag's
  threshold could fall either way on a new request, and the cache freezes
  one of them. That is a fair trade for stable results across runs, and
  `--no-cache` asks fresh. *Doesn't block.*
- **Image templates.** Images are part of the key, so hashing large images
  adds time. Measure it on `receipt-quality`. *Doesn't block.*
