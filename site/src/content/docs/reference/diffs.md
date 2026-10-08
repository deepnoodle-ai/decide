---
title: Diffs
description: Judge what changed rather than whole files, by hunk, function, file, or line.
---

Pipe in a diff to judge what changed rather than whole files:

```sh
git diff main | decide run code-risk --each function # which changed functions are risky?
git diff main | decide run code-risk --each hunk     # which changes?
gh pr diff 42 | decide run code-risk                 # which changed files?
git diff main | decide run prompt-injection          # instructions hidden for AI agents?
git show HEAD | decide run sentiment --each line     # each added line
decide run code-risk change.patch --fail-on flagged  # fail CI on a risky change
```

decide reads a diff from `git diff`, `git show`, `git format-patch`,
`gh pr diff`, or `diff -u`, and any file that ends in `.diff` or `.patch`.

## What one item is

| `--each` | One item is | Named like |
| --- | --- | --- |
| `hunk` (the default) | each block of changed lines | `server.go:42  func (h *Handler) Delete(id string) error {` |
| `function` | each function a change touches, whole | `server.go#L40  Handler.Delete` |
| `file` | every change to one file | `server.go` |
| `line` | each added line | `server.go:43` |

A template that reads whole files, such as `code-risk`, judges each changed
file. Add `--each hunk` to judge each change on its own.

An item is named by the file and the first changed line in the new
version, followed by the function git found above the change, if any. The
model sees the change in diff form, with `-` for removed lines and `+` for
added ones, and whether the file was added, modified, or renamed.

## Changed functions

`--each function` judges each changed function in Go, Python, JavaScript,
TypeScript, or Java as a whole, which gives the model the code around a
change. The model sees the function as it is now, with added lines marked
`+` and removed lines shown as `-`, along with the file's imports.

decide reads the function from the file on disk, so run it in the
repository the diff came from, at the version it describes. When the file
isn't there, such as for `gh pr diff` of another branch, that file is
judged by hunk, with a warning. A change outside any function, such as to
imports or a deleted function, is judged as a hunk. decide reads only
files inside the current folder or its git repository, whatever paths the
diff names.

## What decide skips

decide skips deleted files, binary files, lockfiles such as `go.sum` and
`package-lock.json`, and generated files that start with a `Code generated
... DO NOT EDIT.` or `@generated` comment, and says which. It looks for
that comment in the diff, and in the file on disk when the diff's change is
further down. For a patch of files that aren't on disk, leave out generated
files with `--exclude`. `--include` and `--exclude` match the paths in the
diff, and a lockfile or generated file that `--include` names is judged.

A diff with nothing to judge, such as an empty one from `git diff` when
nothing changed, or one that changes only lockfiles, is not an error:
decide says so and exits 0, so a CI gate on that change passes.

## Several commits and patch files

When a diff holds several commits, as from `git log -p` or `git
format-patch`, each item's name ends with its commit, such as
`server.go@1a2b3c4:42`. decide doesn't read the combined diff that `git
show` prints for a merge commit; diff against one side of it instead, such
as `git diff main...feature`.

A `.diff` or `.patch` file that you name is read as a diff. In a folder,
such files are read as diffs only with `--each hunk`, and their items are
named after the patch, such as `fixes/auth.patch: server.go:42`.
