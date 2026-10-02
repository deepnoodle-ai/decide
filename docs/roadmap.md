# Roadmap

Planned work, grouped by area. Each item is one pull request. Check items
off, or remove them, as they land.

## Templates

- [ ] **Templates.** Rename "skills" to "templates" (#30).
- [ ] **`decide eval`.** Measure a template on labeled examples: agreement
      per question, confident mistakes, suggested thresholds, and
      `--fail-under` for CI. Ship examples with each built-in template.
      Propose the design before building it.
- [ ] **Model pinning.** An optional `model` in `template.json`, and a
      warning when a resumed run is answered by a different model.

## Running and results

- [ ] **`--fail-on`.** `decide run … --fail-on flagged|matched` exits 2 when
      any item is flagged or matched, so CI and hooks can block on it.
- [ ] **Token usage.** Show input tokens in the run summary, in `--json`
      output, and in saved runs.
- [ ] **CSV and Markdown output.** `--format json|csv|md` on `run` and
      `runs view`, keeping `--json` as a shorthand.
- [ ] **Request cache.** An opt-in `--cache` that reuses identical requests.

## Providers and credentials

- [ ] **`decide models`.** List models and confirm the credentials work.
      Cloudflare can't list models, so verify the token instead.
- [ ] **Gateways.** Test Vercel AI Gateway and OpenRouter through
      `TYPESAFE_BASE_URL`, then document them.
- [ ] **Saved credentials.** `decide auth login`, so keys need not be
      exported in every shell.

## Agents

- [ ] **Claude Code plugin.** An agent skill, installable from this
      repository, that teaches agents to write templates and run decide.
- [ ] **MCP server.** `decide mcp`, with tools to run a template or ask a
      question.

## Installing and releases

- [ ] **Releases.** `decide --version`, release binaries and checksums for
      each `v*` tag, and a formula in `deepnoodle-ai/homebrew-tap`.
- [ ] **Install script.** `curl … | sh` that verifies the binary's checksum.

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

## Open questions

- `--fail-on` exit code: 2, or reuse 1?
- Token usage: tokens only, or an estimated cost too?
- Output: `--format`, or separate `--csv` and `--md` flags?
- Agents: a Claude Code plugin, or an install command?
- Install script: ship one, or only Homebrew, binaries, and `go install`?
- Saved credentials: a private file under `~/.decide`, or the system
  keychain?
