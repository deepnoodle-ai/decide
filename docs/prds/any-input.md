# Judge an image, audio, video, or PDF file with any template

Status: Draft
Issue: #111  PRD PR: #113  Implementation PR: none yet  Updated: 2026-11-12
Tech spec: [docs/design/any-input.md](../design/any-input.md)

## Problem

A first-time CLI user has content that is not text: a screenshot of a
diff, a call recording, a scanned form. They want the questions of a
template they already use, such as `code-risk` or `sentiment`, asked
about that file. Today decide refuses:

```
$ decide run code-risk diff.png --provider openai
Error: diff.png is an image, and code-risk reads text

Hint: Images, PDFs, audio, and video need a template that reads them.
See one with: decide templates show receipt-quality
```

The hint sends them to `receipt-quality`, which asks whether a receipt
photo is readable. It does not ask about risk or maintainability. To
get their questions, the user must copy the template, set `"input":
"image"` in it, and keep two copies in step. The provider they chose
reads images. The template's `"input"` field is the only thing in the
way.

#109 gave the library one way to attach a file on every provider
(`decide.WithMedia`), and #108 gave the CLI exact checks for each
provider's kinds and limits. So the parts exist. If we do nothing, the
`"input"` field stays a gate that blocks a job every provider but
TypeSafe can do, and each user who hits it forks a template.

## Context

What the code does today, at c48c1eb:

- **The template's `"input"` picks how every file is read.**
  `source.walker.file` sends every file to the media reader when the
  template's input is media (`internal/source/source.go:442-444`).
  Otherwise a file whose extension or first bytes say it is media goes
  to `notText` (`source.go:445-448`), which is a skip in a folder and a
  `MediaError` for a named file (`internal/source/media.go:219-233`).
  The CLI turns that error into "is an image, and code-risk reads text"
  (`internal/cli/media.go:42-44`).
- **One input per walk.** `source.Options` has one `Input`, one
  `MaxMediaBytes`, and one `MaxPixels` (`source.go:67-90`). The media
  reader looks up the extension in `mediaTypes[in]`, the table for the
  template's input only (`media.go:116`, `source.go:1091-1100`), so a PDF
  named to `receipt-quality` is "not in a format receipt-quality reads".
- **A media item.** The media reader emits one item per file, with state
  `{"path": ..., "content_type": ...}` and the file's path in
  `Item.Image` (`media.go:179-181`). Its unit is the input, such as
  `image`, and the run tally names it ("1 image", `run.go:751-754`).
- **The provider check is per template.** `checkMediaProvider` stops a
  media template on a provider that cannot read its input, before the
  walk and in `--dry-run` (`internal/cli/run.go:169`, `run.go:241-261`).
  `mediaProviders` lists who reads each kind (`run.go:234-239`), and
  `mediaLimit` gives each provider's file limit per kind
  (`run.go:268-279`). Clef's 16-megapixel limit applies to images on
  Cloudflare (`run.go:164-166`).
- **The default provider.** `providerOf` takes `--provider`, then
  `DECIDE_PROVIDER` (the flag's env var, `run.go:85`), then TypeSafe. A
  media template instead runs on the first provider that reads its input
  and has a key set (`run.go:285-304`). `mediaCredentials` explains a
  missing key in that case (`media.go:204-224`).
- **A media template refuses text.** It rejects `--each`, `--field`,
  `--items`, and stdin (`run.go:134-144`, `source.go:754-756`). Binary
  stdin to a text template is a `MediaError` or `NotTextError`
  (`source.go:776-781`). The template file may not set `each` on a media
  template (`internal/template/template.go:359-368`).
- **Saved runs and the cache are per item already.** `Run.add` copies
  each item's media file into the run's `images/` folder
  (`internal/runs/runs.go:156-166`). `evaluate` reads that copy and
  attaches it with `decide.WithMedia` (`runs.go:621-633`, `runs.go:640-655`).
  The cache key hashes the state, the content type, and the bytes of each
  item (`runs.go:666`, `internal/cache/cache.go:90-99`). Nothing in a
  run looks at the template's input, except the worker count:
  `workersFor` lowers the default from 4 to 2 for a media template
  (`internal/cli/app.go:153-165`, also on resume at
  `internal/cli/runs.go:256`).
- **Folders.** A text template's walk counts and skips media files, and
  prints one line for all files that are not text: "Skipped 1 file that
  is not text: d/diff.png" (`source.go:201-204`). If nothing else is
  found, the hint points to media templates (`run.go:653-654`).
- **Docs.** `templates show` says what a template reads
  (`internal/cli/templates.go:153-163`). The site states the template
  rule in `reference/providers.md` ("Files each provider reads"),
  `reference/items.md`, `reference/templates.mdx` (the `"input"` field),
  and `reference/cli.md` (`decide run`, `--provider`, `--workers`).

**Comparable tools.** Simon Willison's `llm` CLI takes a file with
`-a`/`--attachment` for any prompt. The model is chosen first, with
`-m` or the default, and a model that does not take the attachment's
type stops with an error that names the type. It never picks another
model for the file. That is the rule this PRD adopts.

## Users and job

**Primary user:** a first-time CLI user with a file that is not text,
who wants the questions of a template they already know asked about it.
We optimize for this person.

**Job:** "Judge this screenshot, recording, or PDF with the template I
use, on the provider I chose, and tell me before anything is sent if
that provider cannot read it."

**Secondary user:** someone who already runs a media template, such as
`receipt-quality`, and has the receipt as a PDF.

## Concepts

- **Media file.** An image, audio file, video, or PDF. decide sends it
  to the model whole, as one item. Every other file is text.
- **What a template reads** (its `"input"`). The kind of file its
  questions are written for: text (the default), or images, audio,
  video, or PDFs. A template that reads text is a **text template**; one
  that reads images, audio, video, or PDFs is a **media template**. It
  sets what decide picks up in a folder, and what
  `decide templates show` says.
- **A file you name** versus **a folder.** A folder gives a template only
  what it reads. A file you name follows the rule below.

The rule, in two parts, with these words in `templates show`, the
README, and the site:

> A text template also takes an image, audio, video, or PDF file that
> you name. A media template takes only media files.

Either way, the provider you chose must read that kind of file.

## Use cases

### UC-1: Judge a screenshot of a diff with code-risk

```
$ decide run code-risk diff.png --provider openai
Running code-risk on 1 image · openai gpt-6-luna
```

Acceptance:
- [ ] The request to a fake OpenAI server carries `diff.png` as an image
      part, with state `{"path":"diff.png","content_type":"image/png"}`,
      and code-risk's questions.
- [ ] The results and the saved run show `diff.png` with code-risk's
      answers and flags. `decide runs view` and `decide runs resume`
      work on it.
- [ ] `--dry-run` lists `diff.png` as one image and sends nothing.

### UC-2: The default provider reads text only

```
$ decide run code-risk diff.png
Error: diff.png is an image, and TypeSafe reads text only

Hint: Images run on Cloudflare, OpenAI, or Gemini.
Run it with --provider cloudflare, --provider openai, or --provider gemini
```

Acceptance:
- [ ] The fake TypeSafe server records no request, and no run is saved.
- [ ] `--dry-run` gives the same error, with no key set.
- [ ] With `DECIDE_PROVIDER=openai` and no flag, the run works as UC-1.
      decide never picks a provider because of the files.

### UC-3: The chosen provider reads images, not audio

```
$ decide run sentiment call.mp3 --provider openai
Error: call.mp3 is an audio file, and OpenAI reads images, not audio

Hint: Audio runs on Gemini.
Run it with --provider gemini
```

Acceptance:
- [ ] No request is sent and no run is saved.
- [ ] A PDF on Cloudflare says: `report.pdf is a PDF, and Cloudflare
      reads images, not PDFs`, with the hint `PDFs run on Gemini.` and
      `Run it with --provider gemini`.

### UC-4: Text and an image in one run

`notes.txt` has three lines.

```
$ decide run sentiment notes.txt shot.png --provider gemini
Running sentiment on 3 lines and 1 image · gemini gemini-decision-1
```

Acceptance:
- [ ] Three requests carry one line of text each, with no media. One
      request carries `shot.png` as media.
- [ ] `--each file` makes `notes.txt` one item. `shot.png` stays one
      item with any `--each`, `--field`, or `--items`.
- [ ] `--limit 2` judges the first two items in the order named.
      `--sample 2` picks two of the four items.
- [ ] `--json` output and `--dry-run --json` hold four items; the image's
      unit is `image`.
- [ ] A second run answers all four items from the cache. Changing the
      bytes of `shot.png` asks again for that item only.
- [ ] An interrupted run resumes with `decide runs resume`, and the
      image is read from the run's copy.

### UC-5: A folder with images stays text

A repository has Go files and PNG files under `docs/`.

```
$ decide run code-risk .
Skipped 12 files that are not text: docs/a.png, docs/b.png, docs/c.png, and 9 more
Running code-risk on 40 files · typesafe jev-latest
```

Acceptance:
- [ ] On TypeSafe and on OpenAI, the run judges the same 40 files and
      sends no image. A test checks both providers.
- [ ] The "Skipped" line is today's line, unchanged. No tape changes
      because of this track's folder rule.
- [ ] A folder in which a text template finds nothing, and which held
      media files, ends with a hint that names them:

      ```
      $ decide run sentiment shots --provider gemini
      Skipped 3 files that are not text: shots/a.png, shots/b.png, shots/c.png
      Error: Found nothing for sentiment to look at in shots

      Hint: In a folder, sentiment reads only text. To send the images, name them:
      decide run sentiment shots/*.png --provider gemini
      ```

      The glob uses the folder and extension of the first skipped media
      file. `--provider` is the chosen one when it reads that kind, or
      else the first provider that does.

### UC-6: A receipt as a PDF, with receipt-quality

```
$ decide run receipt-quality receipt.pdf --provider gemini
Running receipt-quality on 1 PDF · gemini gemini-decision-1
```

Acceptance:
- [ ] The PDF is sent as media.
- [ ] Without `--provider`, receipt-quality runs on the first image
      provider with a key set, as today. If that is Cloudflare, the run
      stops with `receipt.pdf is a PDF, and Cloudflare reads images, not
      PDFs` and the hint to run it with `--provider gemini`.
- [ ] `decide run receipt-quality photos` still reads only the images in
      the folder.

### UC-7: A media template on a folder, with a provider that cannot read it

```
$ decide run invoice-check invoices --provider openai
Error: invoice-check reads PDF files, which need the gemini provider

Hint: Run it with --provider gemini
```

Acceptance:
- [ ] The run stops before the walk, on TypeSafe and on OpenAI, and in
      `--dry-run`. This is today's message.
- [ ] `decide run invoice-check scan.png --provider openai`, with no
      folder, is valid: the image is sent (UC-6).

### UC-8: Several named files fail

```
$ decide run sentiment call.mp3 intro.mp3 photo.gif shot.png --provider openai
Error: 3 files you named can't be sent
  call.mp3: an audio file, and OpenAI reads images, not audio
  intro.mp3: an audio file, and OpenAI reads images, not audio
  photo.gif: an image, and decide sends images only as PNG, JPEG, or WebP

Hint: Audio runs on Gemini.
Run it with --provider gemini
Save photo.gif as PNG, JPEG, or WebP, and run decide on that file.
```

Each hint is the single-file hint. In the list, a hint that names one
file says its name instead of "it".

Acceptance:
- [ ] Every named media file is checked before any request. One error
      lists each failing file, at most 10, then `and N more`. `shot.png`
      passes and is not listed.
- [ ] Each line gives the reason of the single-file error (R-4, R-6, or
      today's size and pixel errors from #108). The hint holds each
      distinct hint once, in the order of the files.
- [ ] With one failing file, the error is the one-line form of UC-2 and
      UC-3.

## Requirements

- R-1: The provider is chosen before decide looks at any file, by this
  sentence, which `reference/providers.md` and the `--provider` help use
  as written: "decide uses `--provider`, then `DECIDE_PROVIDER`, then
  TypeSafe, or, for a media template, the first provider for its kind
  with a key set." Files never change the provider.
- R-2: A media file that the user names is one item for every template,
  sent with `decide.WithMedia`. Its state is
  `{"path": ..., "content_type": ...}`, as for a media template today. A
  text file that the user names is read as today.
- R-3: For each named media file, decide checks, in this order, before
  any request and before the run is saved: that decide sends its format
  (R-6), that the chosen provider reads its kind (R-4), and the
  provider's size and pixel limits for its kind (today's #108 errors and
  hints). The first failing check is the file's reason. decide checks
  every named media file, then stops with one error if any failed: the
  single-file error for one, or the list of UC-8 for more.
- R-4: A provider that does not read a named file's kind stops the run
  with `<file> is <a kind>, and <Provider> reads <what it reads>`, where
  what it reads is `text only` for TypeSafe, and `images, not <kind>`
  for Cloudflare and OpenAI. The hint is `<Kind> run(s) on <providers>.`
  and `Run it with --provider <p>`, listing each provider that reads the
  kind, in the order Cloudflare, OpenAI, Gemini. `--dry-run` gives the
  same error.
- R-5: The kinds each provider reads come from one table in the CLI,
  the same one `--provider` help, the size hints, and the docs follow:
  TypeSafe none; Cloudflare and OpenAI images; Gemini images, audio,
  video, and PDFs.
- R-6: A named file of a kind decide recognizes but a format it does not
  send, such as `photo.gif`, stops the run on every provider:
  `photo.gif is an image, and decide sends images only as PNG, JPEG, or
  WebP`, with the hint `Save it as PNG, JPEG, or WebP, and run decide on
  that file.` Formats are chosen by extension, as today.
- R-7: A folder gives a text template text only, on every provider.
  Media files in it are skipped with today's "Skipped N files that are
  not text" line. When the walk finds nothing and skipped media files,
  the "Found nothing" error has the hint of UC-5. A media template's
  folder walk picks up files of its own kind, as today. A file in a
  folder that is over the provider's limit is skipped with a "Skipped"
  note, never an error.
- R-8: A media template still refuses a named file that is not media,
  `--each`, `--field`, `--items`, and stdin. The message for a named text
  file becomes `notes.txt is not an image, audio, video, or PDF file,
  and receipt-quality reads images`, with the hint `See the templates
  that read text with: decide templates`.
- R-9: For a media template, when any argument is a folder and the
  chosen provider does not read the template's own kind, the run stops
  before the walk with today's error (UC-7). This holds in `--dry-run`.
  With named files only, the per-file check of R-3 applies instead.
- R-10: Stdin stays text. Binary stdin to a text template stops with
  today's message, and the hint `decide reads images, audio, video, and
  PDFs only from files you name. Save it to a file, and run: decide run
  sentiment FILE`.
- R-11: The default `--workers` is 2 when the run holds any media item,
  and 4 otherwise, on `run` and on `runs resume`. `--workers` wins.
- R-12: `decide templates show` for a text template adds the first half
  of the rule in Concepts: `A text template also takes an image, audio,
  video, or PDF file that you name.` For a media template it adds the
  second half: `A media template takes only media files.` The `run`
  help's item table says media is one item per file you name, or per
  file in a folder for templates that read them.
- R-13: Docs in the same change: the README, the site's
  `reference/providers.md`, `reference/items.md`,
  `reference/templates.mdx`, `reference/cli.md`, the plugin's
  `using-decide` skill where it describes media, each built-in template's
  README that says it reads text, CHANGELOG `## [Unreleased]`, and
  `docs/roadmap.md`. A tape whose output changes is recorded again
  (`TestTapes`). Until #112 measures it, the README and the site show the
  rule with a neutral example, such as `sentiment` on a screenshot of a
  review, not `code-risk` on a screenshot of a diff, and claim nothing
  about accuracy. Tests may use any template.
- R-14: The Go root API does not change. Tests use `decidetest` or a fake
  HTTP server per provider. No live calls.

## Not in scope

- **Media from stdin, URLs, or a record's field.** #104 tracks them.
- **GIF, HEIC, and other formats Gemini takes but decide does not send.**
  #104 lists them.
- **Images on TypeSafe Jev.** #106. When it lands, the table in R-5
  changes, and UC-2 sends a PNG or JPEG.
- **A flag to send media found in a folder.** The maintainer chose
  named files only. A shell glob, such as `docs/*.png`, names many files.
- **Changing what a media template's questions say.** Template text is
  the author's.

## Decisions

Decided by the maintainer:

- **The provider is chosen first, and files never change it.** R-1.
- **A named file of a kind the provider does not read stops the run
  before anything is sent**, and the error names the file, its kind, the
  provider, and the providers that read it. R-4.
- **A file of a kind the provider reads is sent as media, with any
  template.** R-2.
- **A folder gives a template only what it reads.** A text template
  never gets media from a folder, even on a provider that reads it; a
  media file reaches a text template only when the user names it; a
  media template keeps its folder behavior. Reason: text and code are the
  safe default for a directory, and a repository's images should not be
  sent, and paid for, by accident.

Decided by the orchestrator, recorded here:

- **A text file stays text, split as the template says; a media file is
  always one item.** A file has no lines or functions to split.
- **One run may mix text items and media items.** Runs and the cache
  already work per item.
- **Stdin stays text; URLs and media in record fields stay out (#104).**
- **Per-provider size, pixel, and format limits apply as today.**
- **The Go root API does not change.** `decide.WithMedia` already
  attaches a file to any request.

Made in this PRD:

- **The default provider rule for media templates stays.** It is chosen
  from the template and the environment, never from the files, so it
  keeps R-1. Without it, `decide run receipt-quality photos` with only
  `GEMINI_API_KEY` set would fail on TypeSafe. Rejected: TypeSafe as the
  default for every template, which breaks every media template run that
  works today.
- **Skipped media in a folder keep today's line; the "name them" hint
  shows only when the walk finds nothing** (UC-5, R-7). In a repository
  with images, a hint on every `code-risk .` run is noise, and it
  suggests sending diagrams to a code template. When the walk finds
  nothing, the user clearly meant the media. Rejected: a summary line by
  kind with an example command on every run, the first draft.
- **Every failing named file is reported at once** (R-3, UC-8). All
  checks run before any request, so the user can fix all files in one
  pass. Rejected: stop at the first file, which costs one try per file.
- **A media template accepts a named media file of another kind** (UC-6).
  "What a template reads" is no longer a gate, and a receipt or invoice
  often comes as a PDF or a photo. Rejected: keep each media template to
  its own kind, which leaves the gate in place for media templates only.
- **A media template still refuses text, `--each`, `--field`, `--items`,
  and stdin** (R-8). No user has asked to run a photo's questions on
  text, and accepting it brings back stdin and the splitting flags on
  templates whose questions are about a file. Rejected: read a named text
  file as text with any template, which is symmetric but has no use case.
- **The template-wide provider check runs only for folders** (R-9, by
  the orchestrator). A folder gives a media template only its own kind,
  so a provider that cannot read that kind can never find an item. The
  early stop saves a long walk that ends in "Found nothing". A named file
  of another kind stays valid, such as a PNG to `invoice-check` on
  OpenAI. Rejected: drop the check, the first draft; and keep it for
  every run, which blocks UC-6.
- **A format decide does not send is checked before the provider**
  (R-3, R-6). No provider fixes a GIF, so that fix comes first. Rejected:
  provider first, which tells the user to switch provider and then fails
  again.
- **Two workers by default when a run holds any media item** (R-11).
  The limit protects memory per media request, whatever the template.
  Rejected: keep it per template, which sends four large files at once
  in a mixed run.
- **A media item has the same state for every template.** The model sees
  the file's name and type, as with a media template, and one item shape
  is easier to test and cache. Rejected: an empty state, which hides the
  file name the questions may need.
- **Errors use the provider's name, such as TypeSafe, not the model's,
  such as Jev.** The flag and `DECIDE_PROVIDER` take provider names.

## Success

- The five use cases in the brief, and UC-6 to UC-8, pass as tests with
  fake servers.
- No new flag. A user who names a screenshot gets either an answer or an
  error with the command that works.
- Must not get worse: folder runs send the same items as today on every
  provider; a run with no media file sends the same requests; every check
  in AGENTS.md passes.

## Risks and open questions

- **Questions written for text, asked about an image.** code-risk says
  "this code"; the model sees a picture of code. No one has measured the
  answers. #112 is a spike that compares screenshots of diffs with the
  same diffs as text. Until then, the docs use a neutral example and
  claim no accuracy (R-13). Not blocking.
- **A named folder glob can send many files.** `docs/*.png` with 300
  images is 300 paid requests. The confirmation above 100 items still
  asks first. Not blocking.
- **The provider table can drift from the library.** #106 will add
  images on TypeSafe. R-5 keeps the CLI's knowledge in one table, and a
  test sends one file of each kind through each provider's transport and
  checks that it agrees with the table. Not blocking.
