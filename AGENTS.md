# AGENTS.md

Guidance for coding agents and people working in this repository. See
[CONTRIBUTING.md](CONTRIBUTING.md) for what a pull request must include.

## Layout

- `decide` (root): the client, questions, answers, `Eval`, and `Pick`. It
  uses only the standard library and never imports the packages below.
- `decidetest`: a fake server and answer fixtures for tests.
- `backend` selects a provider. `cloudflare` is the Workers AI transport.
- `patterns/<name>`: decisions built from answers, such as gate and rank.
  Each one has a runnable program in `examples/<name>`.
- `cmd/decide` and `internal/`: the CLI. `internal/cli` handles commands
  and output, `internal/template` templates, `internal/source` reading
  data into items, and `internal/runs` saved runs.

Before v1, any API may change. The CLI is experimental.

## Code

- Keep question and answer types open to other packages.
- Preserve API field names and numbers. They are the wire format.
- Validate answers against their questions. Keep thresholds and actions out
  of the root package.
- Pass `context.Context` to network calls. Keep clients safe for
  concurrent use.
- Never log, print, or commit credentials.
- Each example demonstrates one pattern in a short, runnable program.
- Test with `decidetest` or a fake HTTP server. Live tests need the `live`
  build tag and skip without `TYPESAFE_API_KEY`.

## CLI

- Write for someone using it for the first time: few flags, plain words,
  and an example of what to run next. Build with Wonton.
- File names, data, template files, and provider errors are untrusted. Pass
  them through `clean` or `printable` before printing text, and leave
  `--json` output unchanged.

## Docs

Keep the README, [docs/cli.md](docs/cli.md), package comments, and
[examples/README.md](examples/README.md) in step with behavior in the same
pull request. Track planned work in [docs/roadmap.md](docs/roadmap.md), and
update it when an item lands or changes. Keep reviews out of the repository.

## Process

- Work on a branch and open a pull request. Never push to main.
- Propose API and architecture changes before you build them.
- Use small commits with factual messages, such as `fix(cli): ...`.
- Have the change reviewed independently before opening the pull request.
- Merge, release, or change the repository's settings only when a
  maintainer says to.

## Checks

```sh
gofmt -l .
go build ./...
go vet ./...
go test -race -count=1 ./...
go mod tidy && git diff --exit-code go.mod go.sum
```

CI runs the same checks. Do not add a linter configuration without
discussion.
