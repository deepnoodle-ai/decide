---
title: Items
description: What one item is in each kind of data, how decide names it, and how to choose what it reads.
---

decide asks a template's questions about each item in your data. Your data
decides what one item is:

| Data | One item is |
| --- | --- |
| JSONL, JSON, or CSV | each record: a line, an element of an array, or a row named by the header |
| a `.txt` file, or piped text | each line |
| any other text file, such as Markdown or code | the whole file, sent with its path |
| images, for an image template such as `receipt-quality` | each image |
| a diff, such as `git diff` output or a `.patch` file | each hunk: a block of changed lines. See [Diffs](/reference/diffs/). |

## Choose another unit

`--each` chooses what one item is:

| `--each` | One item is | Named like |
| --- | --- | --- |
| `file` | the whole file, even a dataset | `notes.txt` |
| `section` | the text under each Markdown heading | `README.md#install` |
| `paragraph` | each paragraph or list item | `CHANGELOG.md:13` |
| `function` | each function or method in Go, Python, JavaScript, TypeScript, or Java | `models.py#L88  User.save` |
| `line` | each line | `notes.txt:4` |
| `hunk` | each block of changed lines in a diff | `server.go:42` |

```sh
decide run relevance docs -p question="pricing"                    # which docs?
decide run relevance docs --each section -p question="pricing"     # which sections?
decide run code-risk src --each function                           # which functions?
```

The model sees each section or paragraph with the headings above it. A
Markdown file's headings and code blocks are not paragraphs. Some templates
choose a unit for you: `code-risk` reads whole files.

## Functions

With `--each function`, each function, method, and constructor is an item,
named by its line and its name, such as `User.save`. The model sees it with
the comments right above it, the file's imports, and the line that starts
its class. In JavaScript and TypeScript, a top-level statement that holds a
function, such as `app.get("/users", ...)`, is an item, and each test in a
`describe` block is an item, such as `parser › it "reads a header"`. Code
outside functions, such as constants, is not judged. Files in other
languages are skipped. When decide can't follow a file's structure, the
whole file is one item, with a warning.

## Long items

An item too long to judge in one request, about 64 KB of text, is judged
in parts, and the parts' answers are combined into one. A source file is
cut between its functions, and a function too long for one request is cut
at blank lines. An item is flagged when any part is, and the answer shows
the lines of that part. Answers without a flag or match are averaged
across the parts:

```text
src/server.go  judged in 3 parts
! risk             yes           88%  lines 412-655
```

A record too long for one request, such as a CSV row with a long field, is
judged in parts the same way, and the answer names the part, such as
`part 2 of 3`. With `--json`, such an item is still one line, with `parts`
set to the number of parts and `where` giving, for each flagged or matched
question, the part that decided it, such as `"lines 412-655"`.

## Names

Items are named by their path from the current folder, or, for a folder
outside it, from that folder's name, such as `marker/app.py`. The model
sees the same name.

## What decide reads

Folders are read recursively. decide skips hidden files and folders (such
as `.git` and `.env`), files listed in `.gitignore` or `.decideignore`,
binary files, and text files over 1 MiB. Files you name directly are always
read. With no files named, decide reads stdin.

Check what a run will look at before sending anything to the model:

```sh
decide run code-risk . --dry-run
```

A dry run also says how many answers the [cache](/reference/cache/)
already holds.

## Choose your data

```sh
# Only some files. A pattern without a slash matches at any depth.
decide run code-risk src --include '*.go' --exclude '*_test.go'

# Start small.
decide run code-risk . --limit 5               # the first 5 items
decide run sentiment reviews.txt --sample 50   # 50 items picked at random

# Records inside a JSON file, and one field of each record.
decide run ticket-routing export.json --items data.tickets --field body
```

`--items` and `--field` take a dotted path such as `ticket.body`, or a JSON
Pointer such as `/ticket/body`. The model sees only the field; `--json`
output keeps the whole record as `input`.
