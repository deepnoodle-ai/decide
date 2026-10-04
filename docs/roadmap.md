# Roadmap

Planned work, grouped by area. Each item is one pull request. Check items
off, or remove them, as they land.

## Templates

- [x] **Templates.** Rename "skills" to "templates" (#30).
- [x] **`prompt-injection` and `task-readiness`.** Two built-in templates,
      with a demo repository and issues that have planted problems.
- [x] **`pr-description`.** Check pull request titles and descriptions
      against a team's guidelines, with demo pull requests.
- [ ] **More code templates.** `breaking-change`, `secrets`, `sql-injection`,
      and `commit-messages`, each with planted problems in the demo.
- [ ] **`decide eval`.** Measure a template on labeled examples: agreement
      per question, confident mistakes, suggested thresholds, and
      `--fail-under` for CI. Ship examples with each built-in template.
      Propose the design before building it.
- [ ] **Built-in names.** A way to run a built-in template that no project
      or user template can replace. The Claude Code plugin works around it
      by running decide from its own folder.
- [ ] **Model pinning.** An optional `model` in `template.json`, and a
      warning when a resumed run is answered by a different model.

## Running and results

- [x] **Diff input.** `git diff | decide run …` judges each hunk, changed
      file, or added line, and skips lockfiles and generated files.
- [x] **Changed functions.** `--each function` on a diff judges the whole
      function around each change.
- [x] **`--fail-on`.** `decide run … --fail-on flagged|matched` exits 2 when
      any item is flagged or matched, so CI and hooks can block on it. Exit
      1 keeps meaning an error, as with `terraform plan -detailed-exitcode`.
- [ ] **Token usage and cost.** Show input tokens and an estimated cost in
      the run summary, in `--json` output, and in saved runs. Prices come
      from a table of the supported providers' models, dated and linked to
      each provider's pricing page. It can go stale, so label it an
      estimate.
- [x] **Output formats.** `--format json|csv|md|github` on `run`, `runs
      view`, and `runs resume`, keeping `--json` as a shorthand. Markdown is
      for pull request comments and job summaries; `github` writes
      annotations.
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

- [x] **Claude Code plugin.** Installed with `/plugin marketplace add
      deepnoodle-ai/decide`: a command check, a content check, a reply
      check, a judge tool, and a skill that teaches agents to write
      templates and run decide. Other agents can use its `SKILL.md`
      directly.
- [ ] **MCP server.** `decide mcp`, with tools to run a template or ask a
      question.

## Installing and releases

- [x] **Releases.** `decide --version`, release binaries and checksums for
      each `v*` tag, and a formula in `deepnoodle-ai/homebrew-tap`.

## CI

- [ ] **GitHub Action.** `uses: deepnoodle-ai/decide-action@v1`, in its own
      repository with its own `v1` tag, listed on the GitHub Marketplace.
      It installs a pinned release, diffs the pull request against its
      base, and runs a template with `--format github`, with inputs for the
      template, `--each`, `--fail-on`, and an optional pull request
      comment. Build it after `v0.1.0` is tagged and the recipes have run
      on a real repository, and keep it a thin wrapper over the CLI.

## Docs

- [ ] **Demo.** A short README recording made with VHS.
- [x] **Recipes.** GitHub Actions reviews, comments, and gates, issue
      labels, GitLab, pre-commit hooks, and CSV exports.
- [x] **Agent recipes.** The Claude Code plugin, and checking a command
      before any agent runs it.
- [ ] **Badges.** CI, Go Reference, and license, once the repository is
      public.

## Repository

- [ ] Allow squash merges only.
- [ ] Protect `main`. The Claude Code plugin installs from it.
- [x] Make the repository public.
- [ ] Turn on private vulnerability reporting, secret scanning with push
      protection, and Dependabot alerts.
- [x] Add the `TAP_GITHUB_TOKEN` secret.
- [x] Tag `v0.1.0`.

## Not planned

- A full-screen terminal interface. The CLI stays small.
- Packing several items into one request. It saves tokens but mixes
  unrelated text into each judgment.
- Running shell commands chosen from an answer. Actions belong in the
  caller's code.
- Falling back to a text model inside the CLI. Thresholds and fallbacks
  belong in `patterns/gate` and `patterns/funnel`.
- An install script, for now. Homebrew and `go install` come first.
- A hosted GitHub App, for now. The action gives the same results without
  a service that holds keys and reads other people's code. Reconsider
  once the action is in wide use.
