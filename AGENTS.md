# AGENTS.md

Guidance for coding agents working in this repository.

## Layout and guarantees

- Module: `github.com/deepnoodle-ai/decide`. Root package: `decide`.
- Go 1.27 or later. The root package uses only the standard library.
- The root client and `decidetest` follow semantic versioning after v1.
- Packages under `x/` and the CLI are experimental.
- The root package never imports `x/` or CLI packages.
- Describe behavior in package comments, examples, and the README.

## Code

- Keep question and answer interfaces open to external implementations.
- Preserve API field names and numbers. Validate answers against questions.
- Keep application thresholds and action policies outside the root client.
- Use `context.Context` on network calls and keep clients concurrency-safe.
- Never log, print, or commit API credentials.
- Use fake HTTP servers for tests. Live tests require the `live` build tag
  and skip when `TYPESAFE_API_KEY` is absent.
- Examples demonstrate one pattern with a short, runnable program.

## Process

- All changes go through pull requests. Do not push changes directly to main.
- Commit and push when the user requests delivery.
- Use small, buildable commits and factual commit messages.
- Keep PR descriptions concise: behavior added and verification performed.
- Keep process notes and reviews outside the repository.
- Obtain an independent implementation review before opening a PR.
- Propose API or architectural changes before implementation.
- Do not merge PRs, publish releases, or change visibility without a user
  instruction authorizing that action.

## Checks

Run `gofmt`, `go build ./...`, `go vet ./...`, and
`go test -race -count=1 ./...`. Check that `go mod tidy` leaves module
files unchanged. Do not add a linter configuration without discussion.
