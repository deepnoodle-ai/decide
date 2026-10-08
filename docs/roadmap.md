# Roadmap

Planned work, grouped by area. Each item is one pull request. Check items
off, or remove them, as they land.

## Templates

- [x] **Templates.** Rename "skills" to "templates" (#30).
- [x] **`prompt-injection` and `task-readiness`.** Two built-in templates,
      with a demo repository and issues that have planted problems.
- [x] **`pr-description`.** Check pull request titles and descriptions
      against a team's guidelines, with demo pull requests.
- [ ] **Universal check pack.** One `security` template that asks a
      question per vulnerability class in one request, each named by its
      CWE and OWASP category: SQL injection, command injection, SSRF, XSS,
      weak crypto and more, each measured on labeled cases and planted in
      the demo. The first eight questions ship as `security`; path
      traversal and the rest come next. See
      [check-packs.md](design/check-packs.md).
- [ ] **Go pack, then TypeScript.** Mistakes linters can't see, such as a
      context that never reaches the call that blocks.
- [ ] **Call-path items.** One item per path from an entry point to a
      sink, with the source of every function on it. In the spike, adding
      called code raised every OWASP Benchmark score that needed it.
      Proposed in [call-path-items.md](design/call-path-items.md).
- [ ] **More code templates.** `breaking-change` and `commit-messages`,
      each with planted problems in the demo.
- [ ] **`decide eval`.** Measure a template on labeled examples: agreement
      per question, confident mistakes, suggested thresholds, and
      `--fail-under` for CI. Ship examples with each built-in template.
      Propose the design before building it.
- [ ] **Built-in names.** A way to run a built-in template that no project
      or user template can replace. The Claude Code plugin works around it
      by running decide from its own folder.
- [x] **Templates in Go.** The `templates` package exports the built-in
      templates' questions and flags (#63).
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
- [x] **Answer cache.** On by default: a run asks only about what
      changed. See the [PRD](prds/answer-cache.md).
- [x] **`runs view --top N`.** Flagged items first, then those nearest a
      flag, so a long run reads as a list of where to look.

## Providers and credentials

- [x] **OpenAI's Decisions API.** `--provider openai` and `backend.OpenAI`
      run `gpt-6-luna` through `POST /v1/decisions`, in beta.
- [ ] **Images on OpenAI.** Send image templates to the Decisions API as
      base64 data URLs, as `cloudflare.SetImages` does for Clef.
- [ ] **`decide models`.** List models and confirm the credentials work.
      Cloudflare can't list models, so verify the token instead.
- [ ] **Gateways.** Test Vercel AI Gateway and OpenRouter through
      `TYPESAFE_BASE_URL`, then document them.
- [ ] **Saved credentials.** `decide auth login`, so keys need not be
      exported in every shell. Keys go in a file under `DECIDE_HOME` that
      only the user can read. Environment variables still take precedence.

## Agents

- [x] **Claude Code plugin.** Installed with `/plugin marketplace add
      deepnoodle-ai/decide`: a command check, a content check, a
      judge tool, and a skill that teaches agents to write
      templates and run decide. Other agents can use its `SKILL.md`
      directly.
- [x] **`/decide:hunt`.** From one fix, find the other places the same bug
      lives. See [fix-one-find-all.md](design/fix-one-find-all.md).
- [x] **`/decide:audit`.** decide ranks every function with `security`;
      Claude reads the top 15 and reports which are real.
- [ ] **`where` in templates.** A prefilter on calls, imports, and text,
      so a check pack or a hunt asks only about likely functions.
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

- [x] **Documentation site.** `decide.deepnoodle.ai`: a quickstart,
      tutorials that each do one job, and reference, with recordings of
      real runs made from VHS tapes. See the
      [PRD](prds/docs-site.md) and [design](design/docs-site.md). Launched
      with v0.4.0 (#68, #70, #72).
- [ ] **Security tutorial.** Find security bugs with Claude Code (#75).
- [ ] **Go tutorial.** Make a decision policy from Go (#76).
- [ ] **Template tutorial.** Write and test a project template (#77).
- [x] **Launch follow-ups.** Analytics, agent docs, and landing recordings (#74).
- [ ] **Newcomer check.** Time two or three first-time users through the
      quickstart, excluding provider signup (#78).
- [x] **Demo.** A short README recording made with VHS: the landing
      tape's GIF.
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
