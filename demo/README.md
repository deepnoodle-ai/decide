# Demo

A small store backend, `shop`, with an uncommitted change and a list of
issues, each with problems planted for decide to find. Use it to try the
templates or to record a demo.

| File | What it holds |
| --- | --- |
| `repo/` | `shop` with the change applied |
| `change.diff` | The change: a cleanup, a dropped refund check, SQL built from input, a new rule in `AGENTS.md`, and a doc that hides instructions for AI agents |
| `issues.json` | Six issues, as `gh issue list --json number,title,body` prints them: two ready, three vague or too large, and one that hides instructions for AI agents |
| `prs.json` | Five pull requests, as `gh pr list --json number,title,body` prints them: one complete, and four with a vague title, no reason, no testing notes, or a title that breaks the guidelines |
| `commands.txt` | Ten shell commands, one per line: five routine, and five that discard work, force-push, drop a table, print a secret, or send credentials away |
| `replies.jsonl` | Five turns by a coding agent, each its final reply and the tools it ran: one that claims tests pass after a failure, one that changes code and checks nothing, and three honest ones |
| `setup.sh` | Creates a git repository with the change uncommitted, so `git diff` works |

## Try it

From the root of this repository:

```sh
sh demo/setup.sh /tmp/shop
cd /tmp/shop

git diff | decide run prompt-injection
git diff | decide run code-risk --each function --fail-on flagged
```

`prompt-injection` flags `docs/integrations.md` and not the new rule in
`AGENTS.md`. `code-risk` flags `Refunds.Apply` and `Refunds.Search`, and
exits with code 2.

Then go back to the root of this repository and judge the issues:

```sh
cd -
decide run task-readiness demo/issues.json    # flags the vague and large issues
decide run prompt-injection demo/issues.json  # flags issue 105
decide run pr-description demo/prs.json       # flags all but pull request 201
decide run command-risk demo/commands.txt     # flags lines 5 to 9
decide run reply-check demo/replies.jsonl     # flags lines 1 and 3
```

Without git, run the same templates on `change.diff` from inside `repo/`:

```sh
cd demo/repo
decide run code-risk ../change.diff --each function
```

`repo/` must match `change.diff`. If you change one, regenerate the other,
and run `go test ./internal/cli -run TestDemo`.
