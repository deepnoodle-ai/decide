# Roadmap

Planned work, grouped by area. Each item is one pull request. Check items
off, or remove them, as they land.

## Templates

- [x] **Templates.** Rename "skills" to "templates" (#30).
- [ ] **`decide eval`.** Measure a template on labeled examples: agreement
      per question, confident mistakes, suggested thresholds, and
      `--fail-under` for CI. Ship examples with each built-in template.
      Propose the design before building it.
- [ ] **Model pinning.** An optional `model` in `template.json`, and a
      warning when a resumed run is answered by a different model.

## Running and results

- [x] **Diff input.** `git diff | decide run …` judges each hunk, changed
      file, or added line, and skips lockfiles and generated files.
- [ ] **Changed functions.** `--each function` on a diff judges the whole
      function around each change.
- [x] **`--fail-on`.** `decide run … --fail-on flagged|matched` exits 2 when
      any item is flagged or matched, so CI and hooks can block on it. Exit
      1 keeps meaning an error, as with `terraform plan -detailed-exitcode`.
- [ ] **Token usage and cost.** Show input tokens and an estimated cost in
      the run summary, in `--json` output, and in saved runs. Prices come
      from a table of the supported providers' models, dated and linked to
      each provider's pricing page. It can go stale, so label it an
      estimate.
- [ ] **CSV and Markdown output.** `--format json|csv|md` on `run` and
      `runs view`, keeping `--json` as a shorthand. Markdown is for pull
      request comments.
- [ ] **Request cache.** An opt-in `--cache` that reuses identical requests.

## Providers and credentials

- [ ] **`decide models`.** List models and confirm the credentials work.
      Cloudflare can't list models, so verify the token instead.
- [ ] **Gateways.** Test Vercel AI Gateway and OpenRouter through
      `TYPESAFE_BASE_URL`, then document them.
- [ ] **Saved credentials.** `decide auth login`, so keys need not be
      exported in every shell. Keys go in a file under `DECIDE_HOME` that
      only the user can read. Environment variables still take precedence.

## Agents

- [ ] **Claude Code plugin.** An agent skill, installed from this repository
      with `/plugin marketplace add deepnoodle-ai/decide`, that teaches
      agents to write templates and run decide. Other agents can use its
      `SKILL.md` directly.
- [ ] **MCP server.** `decide mcp`, with tools to run a template or ask a
      question.

## Installing and releases

- [ ] **Releases.** `decide --version`, release binaries and checksums for
      each `v*` tag, and a formula in `deepnoodle-ai/homebrew-tap`.

## Docs

- [ ] **Demo.** A short README recording made with VHS.
- [ ] **Recipes.** CI gates, pre-commit hooks, agents, and CSV exports.
- [ ] **Badges.** CI, Go Reference, and license, once the repository is
      public.

## Repository

- [ ] Allow squash merges only.
- [ ] Make the repository public.
- [ ] Turn on private vulnerability reporting, secret scanning with push
      protection, and Dependabot alerts.
- [ ] Add the `TAP_GITHUB_TOKEN` secret and tag `v0.1.0`.

## Not planned

- A full-screen terminal interface. The CLI stays small.
- Packing several items into one request. It saves tokens but mixes
  unrelated text into each judgment.
- Running shell commands chosen from an answer. Actions belong in the
  caller's code.
- Falling back to a text model inside the CLI. Thresholds and fallbacks
  belong in `patterns/gate` and `patterns/funnel`.
- An install script, for now. Homebrew and `go install` come first.
