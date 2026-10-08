# Changelog

Notable changes to decide. The format follows
[Keep a Changelog](https://keepachangelog.com/). Before v1, any release
may change the library API and the CLI.

## [Unreleased]

### Added

- **Docs for agents.** `llms.txt`, Markdown reference pages, and a guide to
  running Decide from an agent. The landing page shows real runs for each
  answer type.

## [0.4.0] - 2026-10-08

decide runs on OpenAI's Decisions API, its built-in templates work from Go,
and its guides live on a documentation site.

### Added

- **OpenAI's Decisions API.** `--provider openai` runs templates on
  `gpt-6-luna` with `OPENAI_API_KEY`; Go code uses `backend.OpenAI`. The API
  is in beta, and image templates don't run on it yet.
- **Built-in templates in Go.** The `templates` package returns a built-in
  template's questions, such as `templates.CommandRisk.Noul("severe")`, and
  flags answers as `decide run` does.
- **A documentation site.** [decide.deepnoodle.ai](https://decide.deepnoodle.ai)
  has a quickstart, three tutorials with recordings of real runs, the
  reference, and recipes. The guides in `docs/` moved there.
- **A Claude Code guide.** The
  [guide](https://decide.deepnoodle.ai/reference/claude-code/) shows how to
  set up the plugin and use `/decide:audit` and `/decide:hunt`.

### Changed

- **`command-risk` flags deploys and releases.** A `publish` question,
  flagged at 80%, replaces `external`, which scored routine pushes high and
  was never flagged. `--json` output has `publish` in place of `external`.
- **A shorter run summary.** `decide run` ends with one line that tells how
  to see the run again, and lists flagged items only when it judged more
  than one.

## [0.3.0] - 2026-10-05

decide audits a codebase for security mistakes, and pays once for each
answer. The plugin drops its reply check.

### Added

- **`decide runs view --top N`.** Show the flagged items first, then those
  nearest a flag, to read a long run from the top.
- **`/decide:audit`.** A plugin skill: decide ranks every function with
  `security`, and Claude confirms the top 15 by reading them.
- **`security`.** A built-in template that asks, in one request per
  function, about SQL and command injection, SSRF, XSS, weak hashes,
  ciphers and random values, and TLS verification turned off.
- **An answer cache.** `decide run` and `runs resume` reuse answers to the
  same question about the same text from the same model name, and ask only
  about what changed, or when a live answer shows the model changed.
- **`/decide:hunt`.** A plugin skill: give it a fix commit or pull request,
  and Claude writes one question about the mistake, tests it on the fix,
  sweeps every function, and confirms the top candidates.

### Changed

- **The plugin checks commands only in bypass mode, and only for severe
  harm.** `command-risk` gains a `severe` question, flagged at 80%. Local
  cleanup, such as `git checkout -- .`, no longer asks.

### Removed

- **`reply-check` and the plugin's reply check.** It flagged about one
  reply in five, mostly honest ones it could not see the evidence for.

## [0.2.1] - 2026-10-03

The Claude Code plugin shows what it checked. The CLI is unchanged.

### Changed

- **The plugin shows its work.** The footer counts checks, each checked
  command or result ends with its verdict, and `/decide` prints a table.
  The status line shows only when a check is off or skipped.

## [0.2.0] - 2026-10-03

Decide comes to Claude Code: a plugin that gets Claude a second opinion
before risky commands, on content from outside, and on its own replies.

### Added

- **`command-risk` and `reply-check`.** Judge a shell command before it
  runs, and check a coding agent's reply against the tools it ran.
- **A Claude Code plugin.** `/plugin marketplace add deepnoodle-ai/decide`
  checks Claude's shell commands, content from outside, and replies, and
  gives Claude a judge tool. Needs Claude Code 2.1.287 or later.

### Changed

- **`prompt-injection` catches steering.** It counts text that tells an AI
  what to recommend, and `hidden` no longer counts text that only
  describes hidden text.
- **Results fit the terminal.** In a terminal, a long score description or
  preview ends with `…` instead of wrapping. Piped output is unchanged.

## [0.1.0] - 2026-10-02

The first release. Decide asks typed questions about your data and gives
answers with probabilities, from the command line or from Go.

### Added

- **The `decide` CLI.** Run a template on files, folders, JSONL, JSON, CSV,
  text, Markdown, source code, and images. Runs are saved and can resume.
- **Nine templates,** from `sentiment` and `triage` to `code-risk` and
  `prompt-injection`. Write your own with `decide templates new`.
- **Diffs.** Pipe `git diff` to judge each changed hunk or function.
- **CI.** `--format` prints CSV, a Markdown report, or GitHub Actions
  annotations. `--fail-on` exits 2 when an item is flagged or matched.
- **The Go library.** `Eval` and `Pick` ask typed questions, and
  `decidetest` fakes the server in tests. Packages under `patterns/`, such
  as `gate` and `rank`, build decisions from answers.
- **Providers.** Jev on the TypeSafe API, Clef on Cloudflare Workers AI, or
  any Jev-compatible service.
- **Install** with `brew install deepnoodle-ai/tap/decide`, or download a
  binary with checksums.

[Unreleased]: https://github.com/deepnoodle-ai/decide/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/deepnoodle-ai/decide/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/deepnoodle-ai/decide/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/deepnoodle-ai/decide/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/deepnoodle-ai/decide/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/deepnoodle-ai/decide/releases/tag/v0.1.0
