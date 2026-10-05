# Spike: call paths

The spike behind [docs/design/call-path-items.md](../../docs/design/call-path-items.md),
run 2026-10-04 on Gogs at `199cf4fd5^` with decide from `main` and Jev
(`jev-1.13.0`). Its results are in that document. Results aren't
committed; `run.sh` writes them under `work/`, which git ignores.

| Path | What it is |
| --- | --- |
| `builder/` | A Go program, built on `golang.org/x/tools`, that writes call-path items by the proposal's rules. It is its own module |
| `run.sh` | Clones Gogs, builds decide and the builder, and runs the four flow templates on the paths |
| `score.py` | Compares the path run with the check-packs spike's function run, label by label |

The templates are the check-packs spike's, in
[`../check-packs/templates`](../check-packs/templates), run without their
`each` default. The function run to compare with is that spike's `v2`
run; `../check-packs/run.sh` writes it.

## Builder

```sh
builder -sink command|path|ssrf|sql [-carry] [-orphans] [-no-deps] [-entry prefix] ./project
```

It differs from the proposal where the spike found a reason:

- `-carry` stops the walk at a function that takes no input (no
  parameters and no captured variables) and at a `go` statement. Without
  it, Gogs' install handler reaches the webhook and mirror goroutines
  through `GlobalInit`.
- The sink lists cover what Gogs uses: `database/sql`, GORM and xorm for
  SQL, and no `code` or `deserialize` kinds.
- It has no tests.

## Labels

The check-packs labels (see its [README](../check-packs/README.md#gogs-labels)),
plus the Gogs side of two fixes made in `git-module`. #8390 (v1.8.8)
changed `DiffBinary`, which `CompareAndPullRequestPost` calls with a pull
request's branch names. #8393 (v1.8.9) changed `LsTree`, `CreateArchive`,
`RevParse` and 15 more, which API handlers such as `getRepoGitTree`,
`getArchive` and `getContents` call with a ref from the request.

The two SQL labels are false positives: `GetByCollaboratorID` and
`searchUserByName` pass `orderBy` to `Order()`, and every caller passes a
constant.

## Run it again

```sh
export TYPESAFE_API_KEY=...
../check-packs/run.sh        # once, for the function run to compare with
./run.sh                     # about 1,600 requests
python3 score.py --carry     # paths plus orphans, with -carry
python3 score.py --carry --no-deps
```

`run.sh` builds with `-carry`. To compare without it, run the builder
without `-carry` into `work/paths-<kind>.jsonl`, run decide into
`work/results/paths-<kind>.jsonl`, and score without `--carry`.
