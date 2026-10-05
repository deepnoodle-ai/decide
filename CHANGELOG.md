# Changelog

Notable changes to decide. The format follows
[Keep a Changelog](https://keepachangelog.com/). Before v1, any release
may change the library API and the CLI.

## [Unreleased]

### Added

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

[Unreleased]: https://github.com/deepnoodle-ai/decide/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/deepnoodle-ai/decide/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/deepnoodle-ai/decide/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/deepnoodle-ai/decide/releases/tag/v0.1.0
