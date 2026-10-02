# Changelog

Notable changes to decide. The format follows
[Keep a Changelog](https://keepachangelog.com/). Before v1, any release
may change the library API and the CLI.

## [Unreleased]

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

[Unreleased]: https://github.com/deepnoodle-ai/decide/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/deepnoodle-ai/decide/releases/tag/v0.1.0
