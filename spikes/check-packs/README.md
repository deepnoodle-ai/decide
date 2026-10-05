# Spike: check packs

The spike behind [docs/design/check-packs.md](../../docs/design/check-packs.md),
run 2026-10-04 with decide from `main` and Jev (`jev-1.13.0`). Its results
and what they mean are in that document. This folder holds what is needed
to run it again. Results aren't committed; `run.sh` writes them under
`work/`, which git ignores.

| Path | What it is |
| --- | --- |
| `templates/` | The templates as last run: the universal pack, with operator flags and config named as trusted, and the Go pack |
| `templates-v1/` | The first wording of the universal templates, which called any input "from outside the program" |
| `paths/` | A Go tool, built on `golang.org/x/tools`, that writes one JSONL item per call path from a Gogs entry point to a sink. It is its own module |
| `build_ctx.py` | Builds the Benchmark items: each test case, and each with the helper code it calls |
| `run.sh` | Clones the code, builds decide, and runs every sweep into `work/results/` |
| `score.py` | Scores the results: `bench`, `gogs`, `paths` and `dive` |

`run.sh` writes five runs to `work/results/`:

- `v1/`: the Benchmark and Gogs, by function, with `templates-v1/`.
- `v2/`: the same, with `templates/`.
- `ctx/`: the Benchmark, with the called helper code in each item.
- `paths/`: Gogs by call path.
- `dive/`: the Go pack, and `golangci-lint.json`.

## Code

| Code | Version | Why |
| --- | --- | --- |
| [OWASP Benchmark](https://github.com/OWASP-Benchmark/BenchmarkJava) | `8b67a88`, Benchmark 1.2 | 2,740 Java test cases, each labeled real or not by CWE |
| [Gogs](https://github.com/gogs/gogs) | `199cf4fd5^` | Before the 2026 security fixes listed below |
| [dive](https://github.com/deepnoodle-ai/dive) | `cae698f` | Our own Go, for the Go pack |

## Gogs labels

Each label is a function a fix changed, at the snapshot before all of
them. #8390 and #8393 were fixed in the `git-module` dependency, so they
have no label in Gogs.

| Template | Function | Fix |
| --- | --- | --- |
| `ssrf` | `HookTask.deliver`, `Request.getResponse` | `199cf4fd5` (#8263): webhook delivery followed redirects |
| `ssrf` | `MigrateRepository`, `Mirror.runSync` | `b9a0093e9` (#8324): migration and mirror sync |
| `command-injection` | `PullRequest.Merge` | `a9dbafbfd` (#8301): argument injection in merge |
| `path-traversal` | `UploadRepoFiles` | `04cb8afbb` (#8332): symlinks in the upload path |
| `path-traversal` | `UserPath`, `RepoPath` | `f6acd4673` (#8334): traversal in owner and repo names |
| `path-traversal` | `isRepositoryGitPath` | `aec2b842c` (#8408): `.git` with a trailing space |

## dive: verified top 10

An agent read each of the top 10 for three Go templates, with callers and
callees. Real means the bug is present and could matter; minor means it
is present but harmless or best effort.

| Template | Real | Minor | False |
| --- | --- | --- | --- |
| `go-goroutine-leak` | 1: `Server.handleConnect` in `experimental/sandbox/proxy`, whose tunnels outlive `Stop` | 3 | 6 |
| `go-context` | 2: `Client.handleOAuthAuthorization` in `experimental/mcp`, a wait with no `ctx.Done()`; `GetShellOutputTool.Call`, a 10-minute wait that ignores cancel | 4 | 4 |
| `go-error-swallowed` | 1: `partFromSource` in `a2a`, a base64 error sent on as an empty file part | 6 | 3 |

## Run it again

```sh
export TYPESAFE_API_KEY=...
./run.sh                      # about 30,000 requests; writes work/results/
python3 score.py bench ctx    # or v1, v2
python3 score.py gogs v2      # or v1
python3 score.py paths
python3 score.py dive
```

The model's answers can shift between runs and model versions, so expect
numbers close to the design doc's, not equal. The answer cache under
`work/home` makes a second run nearly free.
