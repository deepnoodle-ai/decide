---
title: Items
description: What one item is in each kind of data, and how to choose what decide reads.
---

An item is one thing a template's questions are asked about. What counts
as one item depends on the data:

| Data | One item is |
| --- | --- |
| JSONL, JSON, or CSV | each record |
| a `.txt` file, or piped text | each line |
| any other text file, such as Markdown or code | the whole file |
| images, audio, video, or PDFs | each file. See [below](#images-audio-video-and-pdfs). |
| a diff | each block of changed lines. See [Diffs](/reference/diffs/). |

## Choose the unit

`--each` sets what one item is:

| `--each` | One item is | Named like |
| --- | --- | --- |
| `file` | the whole file, even a dataset | `notes.txt` |
| `section` | the text under each Markdown heading | `README.md#install` |
| `paragraph` | each paragraph or list item | `CHANGELOG.md:13` |
| `function` | each function or method | `models.py#L88  User.save` |
| `line` | each line | `notes.txt:4` |
| `hunk` | each block of changed lines in a diff | `server.go:42` |

The first command below asks which docs are about pricing, and the second
asks which sections:

```sh
decide run relevance docs -p question="pricing"
decide run relevance docs --each section -p question="pricing"
```

The model sees a section or paragraph with the headings above it. Some
templates set a unit of their own, such as `code-risk`, which reads whole
files. `--each` overrides it.

## Functions

`--each function` works on Go, Python, JavaScript, TypeScript, and Java.
Each function or method is one item, and the model sees it with its
comments and the file's imports. Files in other languages are skipped.

## Choose your data

Name files or folders, or pipe text in. decide reads folders recursively,
and skips:

- hidden files and folders, such as `.git` and `.env`
- files listed in `.gitignore` or `.decideignore`
- binary files, and text files over 1 MiB

Images, audio, video, and PDFs follow the rules in
[Images, audio, video, and PDFs](#images-audio-video-and-pdfs).

Narrow a run by file name. A pattern without a slash matches at any depth:

```sh
decide run code-risk src --include '*.go' --exclude '*_test.go'
```

Start small, with the first 5 items or 50 picked at random:

```sh
decide run code-risk . --limit 5
decide run sentiment reviews.txt --sample 50
```

For records inside a JSON file, say where they are, and which field to ask
about:

```sh
decide run ticket-routing export.json --items data.tickets --field body
```

To see what a run will read before anything is sent, add `--dry-run`:

```sh
decide run code-risk . --dry-run
```

## Images, audio, video, and PDFs

An image, audio file, video, or PDF is one item, whatever `--each` says.
Which ones a template takes depends on what it reads:

- **A text template also takes an image, audio, video, or PDF file that
  you name.** A folder gives it text only, so a repository's images are
  never sent by accident. One run can mix text and files:

  ```sh
  decide run sentiment notes.txt review.png --provider gemini
  ```

  To send every image in a folder, name them with a pattern:
  `decide run sentiment shots/*.png --provider gemini`.
- **A media template takes only media files.** It reads each file of its
  kind in the folders you name, and skips the rest. It also takes a file
  of another kind that you name, such as a PDF receipt for
  `receipt-quality`.

Either way, the [provider](/reference/providers/#files-each-provider-reads)
must read that kind of file. If it does not, the run stops before anything
is sent, and the error names the providers that do. decide never changes
the provider because of your files. A folder skips a file over the
provider's limit, and says so. A file you name that the provider cannot
take stops the run. One error lists up to 10 such files, then how many
more. decide doesn't read media from stdin, URLs, or paths in records:
download the files first.

The template's questions are asked as written, about the file. No one has
measured how well a question written for text works on a picture of text.

## Long items

An item too long for one request, about 64 KB of text, is judged in parts.
It is flagged when any part is, and the answer shows which part:

```text
src/server.go  judged in 3 parts
! risk             yes           88%  lines 412-655
```
