# Demo

A small store backend, `shop`, with an uncommitted change and a list of
issues, each with problems planted for decide to find. Use it to try the
templates or to record a demo.

| File | What it holds |
| --- | --- |
| `repo/` | `shop` with the change applied |
| `change.diff` | The change: a cleanup, a dropped refund check, SQL built from input, a new rule in `AGENTS.md`, and a doc that hides instructions for AI agents |
| `issues.json` | Six issues, as `gh issue list --json number,title,body` prints them: two ready, three vague or too large, and one that hides instructions for AI agents |
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

Then judge the issues, from the root of this repository:

```sh
decide run task-readiness demo/issues.json    # flags the vague and large issues
decide run prompt-injection demo/issues.json  # flags issue 105
```

Without git, run the same templates on `change.diff` from inside `repo/`:

```sh
cd demo/repo
decide run code-risk ../change.diff --each function
```

`repo/` must match `change.diff`. If you change one, regenerate the other,
and run `go test ./internal/cli -run TestDemo`.
