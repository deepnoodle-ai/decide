# Contributing

Thanks for your interest. Decide is small and deliberate, and we keep it
that way by holding every change to the same bar. Please read this before
you open a pull request. We close pull requests that don't meet it.

## Start with an issue

Open an issue before you write code for anything beyond a small fix. Say
what problem you hit and what you propose. A new public API, a new CLI
command or flag, a new dependency, or a new package needs agreement in the
issue first. Pull requests that arrive with a design nobody agreed to are
usually closed, however good the code.

Typo fixes, broken links, and bug fixes with a failing test can go straight
to a pull request.

## Working with a coding agent

We expect most contributions to be written with Claude Code or a similar
agent, and that's fine. [AGENTS.md](AGENTS.md) is written for your agent and
holds the repository's rules, so point it there first.

You are still the author. Before you open a pull request:

- Read every line of the diff and be able to explain why each one is there.
- Run the checks yourself. Don't rely on the agent's claim that they pass.
- Remove anything you didn't ask for: unrelated refactors, renames,
  reformatting, extra comments, and speculative options.
- Write the pull request description yourself, short and factual.

We don't accept pull requests that read as unreviewed agent output.

## What a pull request needs

- **One purpose.** A fix, or a feature, or a refactor. Split anything else.
- **Tests.** A bug fix includes a test that fails without it. New behavior
  is tested through its public API or the CLI. Tests use `decidetest` or a
  fake HTTP server and never call a live service.
- **Docs in the same change.** Update the README, [docs/cli.md](docs/cli.md),
  package comments, and examples wherever behavior changed.
- **Passing checks:**

  ```sh
  gofmt -l .
  go build ./...
  go vet ./...
  go test -race -count=1 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  ```

- **A short description** of the behavior that changed and how you
  verified it.

## Rules that are easy to miss

- The root package uses only the Go standard library.
- New dependencies anywhere need agreement in an issue first.
- Answers are validated against their questions. Thresholds and actions
  belong to the caller, not the root package.
- The CLI is for newcomers. Prefer plain words, good defaults, and fewer
  flags over more options.
- Text from files, data, skills, or providers is untrusted. Clean it before
  it reaches the terminal.
- Don't claim speed, accuracy, or safety in docs or comments without a
  linked measurement.
- Never commit credentials. Live tests run only with the `live` build tag:
  `TYPESAFE_API_KEY=... go test -tags live ./...`

## Commits

Keep commits small and buildable, with messages such as
`fix(cli): keep ANSI codes out of warnings` or `feat(skill): add a summary
question type`. We squash when merging.

## Security

Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md),
not in an issue.

## License

By contributing, you agree that your contribution is licensed under the
[Apache License 2.0](LICENSE).
