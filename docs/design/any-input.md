# Media files with any template, in the CLI

**Status:** In Review
**Date:** 2026-11-12
**Workflow:** Spec, then build in parallel. The PRD settles the behavior;
this spec settles where the code goes. The build starts on
`feat/any-input`, stacked on the PRD branch.
**PRD:** [docs/prds/any-input.md](../prds/any-input.md) · Issue: #111

## Context

The CLI reads files in `internal/source`. Today one value,
`Options.Input`, decides how the walk reads every file: a media
template sends each file to the media reader, and a text template treats
every media file as "not text" (`source.go:442-448`). Limits come in as
one `MaxMediaBytes` and one `MaxPixels`, for the template's one kind
(`source.go:67-90`). The CLI checks the provider once per template, in
`checkMediaProvider` (`run.go:241-261`).

Below the walk, nothing depends on the template's kind. `runs.Run` copies
each media item into `images/` and attaches it per item with
`decide.WithMedia` (`runs.go:156-166`, `runs.go:621-655`). The cache key
holds each item's content type and bytes (`cache.go:90-99`). So the work
is in the walk and in the CLI's messages. Saved runs, the cache, and the
library stay as they are, except for the worker count.

## Goals

- A named media file is one media item for a text template, and for a
  media template of another kind, when the chosen provider reads it.
- Every named media file is checked before the run is saved and before
  any request, and all failures are reported in one error (PRD R-3).
- Folder walks send the same items as today on every provider.
- No change to the root package, `backend`, the provider packages, or
  the cache.

## Non-goals

- Media on stdin, by URL, or in a record's field (#104).
- New formats, such as GIF and HEIC (#104).
- Images on TypeSafe (#106). This spec keeps the table in one place so
  that #106 changes one line.

## Proposal

### 1. What the provider reads, in `source.Options`

`Options.MaxMediaBytes` and `Options.MaxPixels` give way to one map, from
a media kind to that kind's limits. A kind that is not in the map is one
the provider does not read. A nil map, like an empty one, reads no media,
so a caller that forgets it sends nothing.

```go
// MediaLimits are a provider's limits for one kind of media file.
type MediaLimits struct {
	MaxBytes  int64 // the largest file; 0 means MaxImageBytes
	MaxPixels int64 // the most pixels in an image; 0 means no limit
}

type Options struct {
	Input template.Input // what the template reads; it picks a folder's files
	// Media holds the limits for each kind of media file the provider
	// reads. A named file of a kind not in it is a ProviderError.
	Media    map[template.Input]MediaLimits
	Provider string // the provider's name for messages, such as "OpenAI"
	// ... the other fields, unchanged
}
```

The CLI builds the map from one table, with the providers in the order
the docs and hints use:

```go
// mediaProviders lists the providers that read each media kind, the
// default first. #106 adds "typesafe" to Image.
var mediaProviders = map[template.Input][]string{ /* unchanged */ }

// mediaOptions returns the limits for each kind provider reads.
func mediaOptions(provider string) map[template.Input]source.MediaLimits
```

`mediaOptions` calls `mediaLimit(provider, kind)` for each kind, and sets
`MaxPixels` to `clefMaxPixels` for images on Cloudflare. `mediaLimit`
and the hints (`largerHint`, `pixelsHint`) keep their code.

### 2. How the walk reads a file

`walker.file` changes in one place. Today:

```
Input is media           -> media(path, Input)
extension is media       -> notText
...text readers
```

After:

```
kind := kind by extension (all four tables, then otherImages)
explicit && kind != ""   -> named media file: media(path, kind), with the
                            per-file checks of section 3
!explicit && Input.Media -> media(path, Input), as today (a folder gives
                            a media template its own kind only)
!explicit && kind != ""  -> notText, as today (a folder gives a text
                            template text only)
...text readers, as today
```

For a named file to a media template with no media kind (a text file),
the walk returns `UnsupportedError` with the new R-8 message. A named
file with no extension whose first bytes are media (`notText` with a
sniffed kind) is a `FormatError`, whose hint asks for the extension.

`walker.media` takes the kind as an argument instead of reading
`opts.Input`, and reads its limits from `opts.Media[kind]`. Its item is
unchanged: unit = kind, state `{"path", "content_type"}`, and
`Item.Image` with the path. In a folder, a kind with no entry in
`opts.Media` does not occur, because of section 4.

### 3. The per-file check, and one error for all named files

`Walk` gains a first pass over the arguments that are files, before it
reads anything. For each file whose extension is a media kind, or which
a media template is given, the pass runs the checks that `media` runs
today, plus the provider check, in the PRD's order:

1. Format: an extension in `otherImages`, or no extension -> `FormatError`.
2. Provider: `opts.Media[kind]` is missing -> `ProviderError`.
3. Size, then empty, then the header and pixels -> today's
   `TooLargeError`, `InvalidMediaError`, `PixelsError`.

The pass and `media` share one function, `checkMedia(path, kind, info)
(contentType string, err error)`, so there is one copy of each check.
The pass collects every failure into one error:

```go
// NamedErrors holds the files the user named that cannot be sent, in
// the order named.
type NamedErrors struct{ Errs []error }
```

With one failure, `Walk` returns that error alone, so the single-file
messages and their tests stay. With more, it returns `*NamedErrors`.

The first pass checks every named file, also with `--limit` and
`--sample`, because the error is about the command line, not the items
sent. Files in folders are not part of it; they are skipped with a
note, as today.

The walk itself still checks each media file when it reads it, so a
file that changes between the pass and the read is still caught.

New error types, beside the ones in `media.go`:

```go
// ProviderError reports a named media file of a kind the provider
// does not read.
type ProviderError struct {
	Label    string
	Kind     template.Input
	Provider string
}

// FormatError reports a named file of a media kind in a format decide
// does not send, such as a GIF, or a media file with no extension.
type FormatError struct {
	Label string
	Kind  template.Input
}
```

`MediaError` stays for stdin only.

### 4. The CLI

- **Provider first.** `providerOf` does not change. `run` builds
  `opts.Media = mediaOptions(provider)` where it now sets
  `MaxMediaBytes` and `MaxPixels` (`run.go:161-166`).
- **The folder check.** `checkMediaProvider` runs only when the template
  is a media template and an argument is a folder (`os.Stat` says it is
  a directory). It keeps its message (PRD UC-7, R-9). It runs before
  `--dry-run`, as today.
- **Messages.** `walkError` is split into `explain(err) (line, hint
  string)`, one case per error type, and a wrapper. For one error it
  prints `<file> is <line>` and the hint, as today. For `*NamedErrors`
  it prints `N files you named can't be sent`, one indented line per
  file (`<file>: <line>`), at most 10, then `and N more`, and the
  distinct hints in order. New cases:
  - `ProviderError`: `<noun>, and <Provider> reads text only` for
    TypeSafe, or `reads images, not <kind>` otherwise. Hint:
    `<Kind> run(s) on <providers>.` and `Run it with --provider ...`,
    built from `mediaProviders[kind]` with `orJoin`.
  - `FormatError`: `<noun>, and decide sends images only as PNG, JPEG, or
    WebP`, with today's `unsupportedHint`. With no extension, the hint
    is `Name it with its extension, such as <file>.png.`
  - `UnsupportedError` for a media template: the R-8 text.
  - `TooLargeError` and `PixelsError` read the provider and kind from the
    error, since a run can hold several kinds.
- **Nothing found.** `source.Skipped` gains `Media []SkippedMedia`: the
  label and kind of each media file, of a format decide sends, that a
  text template's folder walk skipped. `nothingFound` adds the UC-5 hint
  when it is set: one pattern per folder and extension, with the folder
  quoted for the shell when it needs it, at most three, and the first
  provider that reads every kind named, the chosen one first. The
  `Skipped ... not text` line does not change.
- **Stdin.** `stdinItems` does not change. The CLI's hint for a
  `MediaError` on stdin becomes the R-10 text.
- **`templates show`.** `reads` adds the PRD's sentence for each kind of
  template.
- **Help.** `runHelp`'s item table and the `--provider` and `--workers`
  help take the PRD's words.

### 5. Saved runs, resume, and workers

Runs already save and send media per item, so a mixed run needs one
change: the worker count.

- `runs.Run` gains `MediaItems int \`json:"media_items,omitempty"\``,
  counted in `Run.add` when `it.Image != nil`. A part of a text item
  never has media, so parts do not count twice.
- `workersFor(c, media bool)` replaces `workersFor(c, template.Input)`.
  `run` passes `run.MediaItems > 0`. `runs resume` passes
  `r.MediaItems > 0 || r.Template.Input.Media()`, so a run saved before
  this change keeps its count.
- `--dry-run --json` encodes the items as today; a media item has its
  unit and state. `--limit` and `--sample` count items of every kind in
  walk order. The cache key needs no change: it already holds the
  content type and bytes.

## Alternatives considered

- **Check providers in the CLI after the walk, from the items.** The walk
  would emit every named media item, and the CLI would reject the ones the
  provider does not read. Rejected: the walk would read and copy files
  into the run before the check, `--limit` would hide later failures,
  and each limit would have two homes.
- **Pass a `func(kind) (MediaLimits, bool)` instead of a map.** It hides
  the table from tests and adds nothing a map lacks. Rejected.
- **Collect errors during the walk, without a first pass.** `--limit`
  stops the walk early, and a folder before the named files can be slow.
  The pass reads at most 512 bytes and an image header per named file.
  Rejected.
- **Keep `MaxMediaBytes` and `MaxPixels`, and add a kind set.** Two
  fields that must agree, and Clef's pixel limit is for images only.
  Rejected.

## Tradeoffs and consequences

- A named file is opened twice, once in the pass and once in the walk.
  It costs one more header read per named file.
- `--limit 1 a.png b.mp3` on OpenAI fails because of `b.mp3`, though
  `b.mp3` would not be sent. The error is about what the user typed, and
  is easier to predict than one that depends on `--limit`.
- `Options.MaxMediaBytes` and `Options.MaxPixels` go. They are in
  `internal/`, so no user sees it.
- The `template-preview` tape shows `decide templates show`, so R-12
  changes its output. It is recorded again as `docs/releasing.md` says.
  No other tape runs a media file or a folder with media.

## Test plan

All tests use `httptest` servers through `*_BASE_URL`, as
`openai_images_test.go` does, or `decidetest`. No live calls, and fake
keys only.

| Case | Test | Checks |
| --- | --- | --- |
| UC-1 | `TestRunTextTemplateOnNamedImage` (cli) | `code-risk diff.png --provider openai`: one request with an `input_image` part and state `{"path","content_type"}`; `runs view` and `runs resume` show it; `--dry-run` lists 1 image and sends nothing. |
| UC-2 | `TestNamedImageOnTypeSafe` (cli) | exact error and hint; zero requests on a fake TypeSafe server; no run directory; same error with `--dry-run` and no key; with `DECIDE_PROVIDER=openai` it runs. |
| UC-3 | `TestNamedMediaKindProviderCannotRead` (cli) | `call.mp3` on OpenAI and `report.pdf` on Cloudflare: exact errors and hints; zero requests. |
| UC-4 | `TestMixedRun` (cli) | `sentiment notes.txt shot.png --provider gemini`: 3 requests without media and 1 with; `--each file` gives 2 items; `--limit 2` and `--sample 2`; `--json` and `--dry-run --json` hold 4 items with unit `image` on one; a second run is all cache hits; changed PNG bytes ask again for that item only; an interrupted run resumes and reads the run's copy; `MediaItems` is 1 and workers default to 2. |
| UC-5 | `TestFolderWithImagesStaysText` (cli) | `code-risk .` on TypeSafe and OpenAI: same items, no media, today's Skipped line. A folder with only images: the UC-5 hint, with `--provider` set as the PRD says. |
| UC-6 | `TestMediaTemplateOtherKind` (cli) | `receipt-quality receipt.pdf --provider gemini` sends a PDF; with only a Cloudflare key, the Cloudflare error; `receipt-quality photos` reads images only. |
| UC-7 | `TestMediaTemplateFolderWrongProvider` (cli) | `invoice-check invoices` on TypeSafe and OpenAI stops before the walk, also with `--dry-run`; `invoice-check scan.png --provider openai` runs. |
| UC-8 | `TestSeveralNamedFilesFail` (cli), `TestWalkNamedErrors` (source) | the list, the order, the cap of 10 with `and N more`, one hint per distinct hint; a passing file is not listed; `--limit 1` still checks every named file. |
| R-6, R-8, R-10 | `TestNamedFormatErrors`, `TestMediaTemplateRefusesText`, `TestBinaryStdinHint` (cli) | GIF and no-extension messages; the R-8 message; the R-10 hint. |
| R-5 | `TestMediaTableMatchesTransports` (cli) | for each provider and kind, a 1-pixel PNG, a WAV header, a short MP4 header, and `%PDF-` sent through `backend.NewClient` to a fake server: `errors.ErrUnsupported` exactly when the table says the provider does not read the kind. |

Existing tests in `media_test.go` and `media_inputs_test.go` that expect
"is an image, and sentiment reads text" for a named file change to the
new messages. Tests for folders and stdin keep their text.

## Order of commits

1. `refactor(source)`: `Options.Media` and `MediaLimits`; `media` takes
   the kind. No behavior change; existing tests pass.
2. `feat(cli)`: named media files with any template, the first pass, the
   new errors, the folder-only provider check, workers. Tests for UC-1 to
   UC-8.
3. `docs`: README, site pages, plugin skill, template READMEs, CHANGELOG,
   roadmap, and the `template-preview` tape.

## Rollout

The CLI is experimental, and this change only turns errors into runs.
No flag. Saved runs from before the change resume as before.

## Open questions

None that change the design. #112 decides which example the docs show
after it measures screenshots of diffs.
