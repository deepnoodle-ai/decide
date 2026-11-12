---
title: Diffs
description: Judge only what changed, by hunk, function, file, or line.
---

Pipe in a diff to judge only what changed:

```sh
git diff main | decide run code-risk --each function
```

decide reads a diff from `git diff`, `git show`, `git format-patch`, or
`gh pr diff`, and any `.diff` or `.patch` file.

## Choose the unit

| `--each` | One item is | Named like |
| --- | --- | --- |
| `hunk` | each block of changed lines | `server.go:42` |
| `function` | each function a change touches, whole | `server.go#L40  Handler.Delete` |
| `file` | every change to one file | `server.go` |
| `line` | each added line | `server.go:43` |

The default is `hunk`, unless the template sets a unit of its own.
`code-risk` judges each changed file:

```sh
gh pr diff 42 | decide run code-risk
gh pr diff 42 | decide run code-risk --each hunk
```

## Changed functions

`--each function` judges the whole of each function a diff touches, so the
model sees the code around the change. It works on Go, Python, JavaScript,
TypeScript, and Java.

Run it in the repository the diff came from. decide reads each function
from the file on disk.

## What decide skips

decide skips deleted files, binary files, lockfiles, and generated files,
and says which. When a diff has nothing left to judge, decide exits 0.
