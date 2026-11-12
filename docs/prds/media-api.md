# Attach a file the same way on every provider

Status: Draft
PRD PR: TBD  Implementation PR: TBD  Updated: 2026-11-12

## Problem

A Go developer asks typed questions about things that are not text: a
receipt photo, an invoice PDF, a call recording. Today they must learn
three helpers with three wire shapes: `cloudflare.SetImages`,
`openai.SetImages`, and `gemini.SetMedia`. Each one writes a different
value into `Request.Extra`. Code that attaches a photo for Clef does not
work on Gemini, though `backend.NewClient` promises that "changing Config
selects the backend without changing request code".

Worse, a request sent to the wrong provider can succeed. An audit at
972a70f sent a PDF attached with `gemini.SetMedia` to OpenAI. The
transport posted `{"input":"...","media":[...]}`, the API ignored the
unknown field, and the answers came from the text alone, with no error.
TypeSafe and Cloudflare forward unknown media the same way.

Large files have three more problems:

- The default 10-second attempt timeout cuts off a slow upload, and the
  client retries it. One audit run uploaded 12 MiB for a 4 MiB file.
- `WithLogBodies` writes every file, in base64, to the log on every
  attempt.
- One attempt with a 30 MiB file allocates 382 MiB on Gemini and 631 MiB
  on OpenAI, because the request is encoded, decoded, and encoded again.

If we do nothing, each Go caller writes the provider switch that the CLI
already carries, and a wrong provider gives quiet, wrong answers.

## Context

**Our code.** There is no media type in the root package. `Eval` and
`Pick` can attach media only through `WithRequestExtra`. Each provider
package validates differently: Clef checks image headers and limits;
OpenAI checks no size or count and reads only 12 bytes of a WebP; Gemini
trusts the declared MIME type, rejects common aliases such as
`video/quicktime` and `Image/PNG`, and accepts `http://`. A `[]byte`
nested in state is sent as base64 text. `decidetest` cannot record media
in a form a test can check. Go programs cannot ask a built-in template
which kind of file it reads. `funnel.Shared` cannot carry media.

**What the providers take** (public docs, read 2026-11-12):

- **TypeSafe (Jev).** PNG and JPEG images, in preview ("Dreamer-ready").
  A request lists each image under `assets` with a reference string and
  writes that reference in the state. It must send the header
  `TypeSafe-Danger-Zone-Images: true`. Multipart upload is recommended.
  Each file is at most 16 MiB and 280 to 1568 pixels a side; the body is
  at most 32 MiB. Out-of-range images are rejected, not resized. No audio,
  video, PDF, or URL. [Images guide](https://docs.typesafe.ai/images.md)
- **Cloudflare (Clef).** At most four PNG, JPEG, or WebP images, placed
  before the state: 4 MiB and 16 megapixels each, 8 MiB in total, 13 MiB
  per request. No URLs, no video in the hosted API.
  [Clef](https://developers.cloudflare.com/workers-ai/models/clef/)
- **OpenAI (GPT-6 Luna).** Images as base64 data URLs or public HTTP(S)
  URLs. "Files, audio, and item references aren't supported." The
  Decisions pages list no image formats or limits; the general vision
  guide lists PNG, JPEG, WebP, and non-animated GIF, 512 MB and 1,500
  images per request.
  [Decisions](https://developers.openai.com/api/docs/guides/decisions)
- **Gemini (Gemini Decision 1).** Images, audio, video, and PDFs, inline
  or by URI (Files API URI or HTTPS URL). Inline data is at most 100 MB
  of base64 per request, or 50 MB for PDFs. No YouTube, no `gs://`.
  [Decisions API](https://aistudio.google.com/docs/decisions-api)

**Comparable libraries.** Google's Gen AI Go SDK takes raw bytes or a
URI plus a MIME type (`genai.NewPartFromBytes(data, mimeType)`,
`genai.NewPartFromURI(uri, mimeType)`) and derives the kind from the
MIME type. Anthropic's and OpenAI's Go SDKs have one constructor or part
type per kind. Of two cross-provider libraries, Fantasy takes
`FilePart{Data []byte, MediaType}` and drops media a provider cannot
take, with a warning; Dive returns an error. None exports a
per-provider media helper.

## Users and job

**Primary user:** a Go developer who asks typed questions about files,
and who wants to choose or change the provider in configuration. We
optimize for this person.

**Job:** "Attach this file to my question, on whichever provider I
configured, and tell me at once if that provider cannot read it."

**Secondary users:** the decide CLI, which attaches one file per item;
test authors who check what their code sent.

## Concepts

- **Media.** A file that a question is about: an image, audio clip,
  video, or PDF. A request carries media alongside its state. Media is
  the file's bytes, or a URL the provider fetches, plus its MIME type.
- **Media kind.** One of `image`, `audio`, `video`, or `pdf`. decide
  derives it from the MIME type. Providers support media by kind, then by
  format.

Media does not change questions or answers. A question about media reads
it as part of the item, as it reads the state.

## Proposed API

Root package `decide` (standard library only):

```go
// MediaKind is the kind of file a request carries.
type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaAudio MediaKind = "audio"
	MediaVideo MediaKind = "video"
	MediaPDF   MediaKind = "pdf"
)

// Media is a file a request carries with its state: an image, audio clip,
// video, or PDF. Set Data to send the file's bytes, or URL to have the
// provider fetch it. MIMEType is required in both cases.
type Media struct {
	MIMEType string // such as "image/png" or "application/pdf"
	Data     []byte // the file's bytes; not copied, so do not change them during a call
	URL      string // an https URL, such as a Gemini Files API URI
}

// Kind returns the kind of m's MIME type, or "" for a type decide does
// not send.
func (m Media) Kind() MediaKind

// String describes m without its bytes, such as "image/png, 48213 bytes".
func (m Media) String() string

type Request struct {
	State     any
	Model     string
	Questions map[string]Question
	Media     []Media // new: files sent with the state, in order
	Extra     map[string]any
}

// WithMedia appends files to the request.
func WithMedia(media ...Media) RequestOption
```

Other packages:

```go
// decidetest: Recorded.Request.Media holds the files a request carried,
// in order, with the state as the caller built it.

// PNG returns a small PNG that the default provider accepts.
func PNG() decide.Media

// templates: Media returns the kind of file the template reads, or ""
// when it reads text.
func (t *Template) Media() decide.MediaKind

// patterns/funnel: Shared takes request options, such as WithMedia, for
// every request it builds.
func Shared[T any](state any, questions func(Item[T]) (map[string]decide.Question, error),
	size int, opts ...decide.RequestOption) Layout[T]
```

## Use cases

### UC-1: Ask about a receipt photo, then switch provider

The developer reads the file and asks with `Eval`:

```go
photo, err := os.ReadFile("receipt.jpg")
if err != nil {
	return err
}
client, err := backend.NewClient(backend.Config{
	Provider:  backend.Cloudflare,
	APIKey:    os.Getenv("CLOUDFLARE_AUTH_TOKEN"),
	AccountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
})
if err != nil {
	return err
}
e, err := decide.Eval(ctx, client, "A photo of a store receipt.",
	decide.Noul("Can you read every line item and the total?"),
	decide.WithMedia(decide.Media{MIMEType: "image/jpeg", Data: photo}))
```

Then they change only the config to
`backend.Config{Provider: backend.Gemini, APIKey: os.Getenv("GEMINI_API_KEY")}`.
The `Eval` call does not change.

Acceptance:
- [ ] The same `Eval` call sends the photo on TypeSafe, Cloudflare,
      OpenAI, and Gemini, each in its own wire form (see the provider
      table). A test per provider checks the body a fake server receives.
- [ ] `decide.Pick` takes `WithMedia` the same way.
- [ ] `Request.Media` is nil when no media is attached, and a request
      without media is sent exactly as today on every provider.

### UC-2: Ask about an invoice PDF; see a clear error on OpenAI

```go
pdf, err := os.ReadFile("invoice.pdf")
if err != nil {
	return err
}
invoice := decide.Media{MIMEType: "application/pdf", Data: pdf}
e, err := decide.Eval(ctx, client, "An invoice from a supplier.",
	decide.Noul("Does the total match the sum of the line items?"),
	decide.WithMedia(invoice))
if errors.Is(err, errors.ErrUnsupported) {
	// This provider cannot read PDFs. Nothing was sent.
}
```

On Gemini, the call returns an answer. On OpenAI, it returns at once:

```
unsupported operation: openai does not take PDFs (media[0], application/pdf); it takes PNG, JPEG, and WebP images, as bytes or by URL
```

Acceptance:
- [ ] On OpenAI, the error matches `errors.Is(err, errors.ErrUnsupported)`,
      names the provider, the kind, the file's position and MIME type, and
      what the provider takes.
- [ ] A fake server records no request: the client makes no attempt and
      no retry.
- [ ] The error does not match `decide.ErrInvalidRequest`, so a caller
      can tell "this provider cannot" from "this request is wrong".

### UC-3: Attach a video by URL on Gemini

```go
clip := decide.Media{MIMEType: "video/mp4", URL: "https://storage.example.com/clip.mp4"}
e, err := decide.Eval(ctx, client, "Security camera, loading dock.",
	decide.Noul("Does the video show a person on the dock?"),
	decide.WithMedia(clip))
```

Acceptance:
- [ ] Gemini sends a `video` block with `uri` and `mime_type`, after the
      state's text block.
- [ ] A Files API URI (`https://generativelanguage.googleapis.com/...`)
      works the same way.
- [ ] `http://`, `gs://`, and YouTube URLs (`youtube.com`, any
      `*.youtube.com`, `youtu.be`) fail before sending.
      `http://` and `gs://` fail on every provider with
      `ErrInvalidRequest`; YouTube fails on Gemini with `ErrUnsupported`.
- [ ] On TypeSafe and Cloudflare, any URL fails with `ErrUnsupported`:
      "cloudflare does not take media by URL (media[0]); attach the
      file's bytes in Data". On OpenAI, a video URL fails as a video, and
      an image URL is sent.

### UC-4: Test that my code attached one PNG

```go
func TestReceiptCheck(t *testing.T) {
	srv := decidetest.NewServer(t)
	client := srv.Client(t)
	if err := checkReceipt(ctx, client, decidetest.PNG()); err != nil {
		t.Fatal(err)
	}
	media := srv.Requests()[0].Request.Media
	if len(media) != 1 || media[0].MIMEType != "image/png" {
		t.Fatalf("media = %v, want one PNG", media)
	}
}
```

Acceptance:
- [ ] `Recorded.Request.Media` holds each file in order, with its MIME
      type and bytes; `Recorded.Request.State` is the state the caller
      built, not the form sent on the wire.
- [ ] The fake server rejects media the TypeSafe API rejects (a missing
      images header, an unreferenced asset, a format other than PNG or
      JPEG) with the same status. A client built by `decidetest` rejects
      audio, video, PDF, and URLs locally, as TypeSafe does.
- [ ] `decidetest.PNG()` passes the default provider's checks.
- [ ] Docs show how to test media code for other providers: point
      `backend.Config.BaseURL` at an `httptest.Server`. Unsupported media
      never reaches it; supported media arrives in the provider's wire
      form.

### UC-5: Route the built-in templates that read media

```go
for _, t := range templates.All() {
	switch t.Media() {
	case "":
		textTemplates = append(textTemplates, t)
	case decide.MediaPDF, decide.MediaAudio, decide.MediaVideo:
		geminiTemplates = append(geminiTemplates, t)
	default:
		imageTemplates = append(imageTemplates, t)
	}
}
```

Acceptance:
- [ ] `templates.ReceiptQuality.Media()` is `decide.MediaImage`,
      `templates.InvoiceCheck.Media()` is `decide.MediaPDF`, and every
      text template returns `""`.
- [ ] The `templates` package comment names `decide.WithMedia`, not
      provider helpers.

## Provider table

What each provider does with each kind. "Rejects" means the call returns
an error wrapping `errors.ErrUnsupported` before any network call.
A limit that is broken returns `decide.ErrInvalidRequest`, also before
any network call.

| | TypeSafe (Jev) | Cloudflare (Clef) | OpenAI (GPT-6 Luna) | Gemini (Decision 1) |
|---|---|---|---|---|
| Image, bytes | PNG, JPEG. Sent as a multipart upload with the images header. | PNG, JPEG, WebP. | PNG, JPEG, WebP. | PNG, JPEG, WebP, HEIC, HEIF, GIF, BMP, TIFF. |
| Image, URL | Rejects. | Rejects. | Sends the URL. | Sends the URL. |
| PDF | Rejects. | Rejects. | Rejects. | Bytes or URL. |
| Audio | Rejects. | Rejects. | Rejects. | Bytes or URL. |
| Video | Rejects. | Rejects. | Rejects. | Bytes or URL; no YouTube. |
| Other formats of a taken kind | Rejects (such as WebP, GIF). | Rejects. | Rejects (such as GIF). | Rejects a type not listed. |
| Where files go | The state is sent as `{"state": <state>, "media": [<ref>, ...]}`; each ref names one uploaded image. | Images before the state, as Clef places them. | One user message: the state as text, then one image part per file. | The state as a text block, then one block per file. |
| Limits checked before sending | 16 MiB a file; 280 to 1568 pixels a side; 78,400 to 2,458,624 pixels; still PNG; 32 MiB body. | 4 images; 4 MiB and 16 MP each; 8 MiB total; 13 MiB body. | 1,500 images; 512 MB body. | 100 MB of inline base64; 50 MB when any PDF is inline. |

On every provider:

- MIME types are matched without case and without parameters, and these
  aliases are accepted: `image/jpg` → `image/jpeg`; `audio/x-wav`,
  `audio/wave`, `audio/vnd.wave` → `audio/wav`; `audio/x-m4a`,
  `audio/mp4` → `audio/m4a`; `video/quicktime` → `video/mov`.
- When the bytes have a format decide recognizes (PNG, JPEG, GIF, WebP,
  PDF, WAV, MP3, OGG, FLAC, MP4, WebM), it must match the declared type.
- Files are sent in the order they were attached.

## Requirements

- R-1: `Request.Validate` checks each `Media` and returns an error
  wrapping `ErrInvalidRequest` when: `MIMEType` is empty or not an image,
  audio, video, or PDF type; both or neither of `Data` and `URL` are set;
  `URL` is not an absolute `https` URL with a host; or the bytes contradict
  the declared type. Messages name the file as `media[i]`.
- R-2: Each provider's transport sends every file in its own wire form, or
  rejects the request before any network call with an error wrapping
  `errors.ErrUnsupported`. The message has the form
  `<provider> does not take <kind or form> (media[i], <MIME type>); it takes <what it takes>`.
  No transport forwards media it cannot send.
- R-3: `Request.Extra` keys `images`, `media`, `videos`, and `assets` are
  reserved. `Request.Validate` rejects them:
  `Extra key "images" is reserved; attach files with decide.WithMedia`.
  Other keys pass through as today.
- R-4: `Request.MarshalJSON` returns an error for a request with media:
  `decide: a request with media has no JSON form; a transport sends Request.Media itself`.
  A custom transport that encodes the request as JSON fails instead of
  dropping the files. The `Transport` doc says a transport that cannot send
  `req.Media` returns `errors.ErrUnsupported`.
- R-5: A request with media gets a 5-minute attempt timeout by default.
  `WithAttemptTimeout` and `WithCallAttemptTimeout`, when given, win. A
  request without media keeps the 10-second default.
- R-6: With `WithLogBodies`, the request body in the log has no file
  bytes. A `media` attribute lists each file by `Media.String()`. The log
  doc says the body is decide's request, not the provider's wire body.
- R-7: One attempt allocates at most 4 times the file's size on
  Cloudflare, OpenAI, and Gemini, and at most 2 times on TypeSafe, for a
  30 MiB file. A benchmark in each package checks it. The client does not
  encode media to catch errors or to log.
- R-8: `Request.Validate` rejects a `[]byte` at any depth of the state,
  instructions, and criteria, through maps, slices, arrays, pointers, and
  struct fields. `json.RawMessage` is allowed. The message names the path
  and the media API:
  `state.receipt is []byte, which would be sent as base64 text; attach files with decide.WithMedia, or pass text as a string`.
- R-9: The TypeSafe transport sends `TypeSafe-Danger-Zone-Images: true`
  only on a request that carries media, and generates a new random
  reference for each file in each request.
- R-10: The CLI attaches media with `decide.Media` and keeps its current
  behavior: the same providers per template, the same file limits, and
  the same 5-minute attempt timeout, now from the library. Only the lines
  that attach media and check provider support change.
- R-11: Docs in the same change: README "Use it from Go", the site's
  `start/go.mdx` and `tutorials/decide-from-go.mdx`, the root and
  provider package comments, the roadmap "Media by URL" line, and
  CHANGELOG. An `ExampleWithMedia` test and an `examples/media` program
  run against `decidetest`.

## Removed

No shims and no deprecated aliases:

- `cloudflare.Image`, `cloudflare.NewImage`, `cloudflare.SetImages`.
- `openai.Image`, `openai.NewImage`, `openai.SetImages`.
- `gemini.Media`, `gemini.Kind` and its constants (`KindImage`,
  `KindAudio`, `KindVideo`, `KindDocument`), `gemini.NewMedia`,
  `gemini.MediaURI`, `gemini.MediaKind`, `gemini.SetMedia`, and the
  `Resolution` field.
- The `images`, `media`, and `videos` keys of `Request.Extra` as a way to
  attach media (now reserved, R-3).
- The CLI's own media timeout constant, replaced by R-5.

## Not in scope

- Media inside question instructions or criteria. TypeSafe and OpenAI take
  it; Gemini and Clef do not. It is a later, separate feature.
- A Files API upload helper. Callers upload with Google's SDK and pass the
  URI in `URL`.
- Per-file quality settings (Gemini `resolution`, OpenAI `detail`). The
  defaults serve the use cases; see Decisions.
- TypeSafe Box questions.
- Adding TypeSafe to the CLI's providers for image templates. The CLI keeps
  its current defaults; the CLI track can add it.
- Reading media from an `io.Reader`. Retries resend the body, so the
  bytes must be held anyway.
- OpenAI's newer features (JSON input parts, 400 questions).

## Decisions

Made by the orchestrator, recorded here:

- Media is a root-package concept, reached through a `RequestOption` and a
  `Request` field; the root package stays standard-library only.
- Each transport translates media or rejects it with `errors.ErrUnsupported`.
- The per-provider helpers and the `images`/`media`/`videos` conventions
  go, with no shims. `Request.Extra` stays a pass-through for other keys,
  and the former media keys are reserved.
- The TypeSafe transport supports media only if Jev does; media by URL
  only where the API takes it; no Files API helper; no PDF or GIF on
  OpenAI unless OpenAI documents it.
- The CLI moves with the smallest diff.

Made in this PRD:

- **TypeSafe sends PNG and JPEG images.** Jev 1.13 and later take them,
  per TypeSafe's images guide. The guide marks images as a preview, so the
  package comment says so. Rejected: rejecting all media locally, which
  would hide a documented capability of the default provider.
- **TypeSafe wraps the state as `{"state": ..., "media": [...]}`.** Jev
  needs each image's reference written inside the state. Wrapping keeps a
  structured state structured and gives one rule for every state type.
  Rejected: appending references to the state's text, which turns an
  object state into text only when media is present; adding a member to
  the caller's object, which can collide with their keys.
- **TypeSafe uses a multipart upload.** TypeSafe recommends it; it sends
  the bytes as they are, with no base64, which saves a third of the 32 MiB
  body limit and most of the memory. Rejected: `data_base64` in JSON.
- **TypeSafe checks image size and dimensions locally.** The API rejects
  out-of-range images rather than resizing them, so an early, specific
  error saves an upload. Rejected: leaving it to the server.
- **OpenAI takes images by URL.** The Decisions guide states it, so "media
  by URL" is not Gemini-only.
- **OpenAI does not take GIF or PDF.** The Decisions pages say files are
  not supported and list no image formats. Only the general vision guide,
  written for other endpoints, lists GIF. We take the conservative path
  and reject locally.
- **OpenAI applies the vision guide's limits** (1,500 images, 512 MB per
  request). They are OpenAI's only documented image limits, and they stop
  a runaway request from building in memory.
- **Gemini's 50 MB rule applies to inline data only.** The docs say
  "Inline data accepts base64-encoded bytes (up to 100 MB per request, or
  50 MB for PDFs)". A PDF by URL is not inline, so it does not lower the
  limit. When any PDF is inline, all inline data together must fit in
  50 MB, which is the stricter reading.
- **Kind comes from the MIME type.** One field cannot disagree with
  another. Google's Go SDK does the same. Rejected: a separate `Kind`
  field.
- **`Data` is `[]byte`, not base64.** Callers have bytes; each transport
  encodes once, in the form it needs. Rejected: a base64 string, which
  every caller would encode and every transport would decode to check.
- **URLs must be `https`.** Gemini documents HTTPS only; a public `http`
  URL is a weak default. Rejected: `http` on OpenAI only.
- **No per-file quality setting.** Gemini's `resolution` and OpenAI's
  `detail` have different values, and no use case needs them. If a user
  asks, the home is the provider's `Config`, set once per client, because
  the root type must not carry one provider's settings. Rejected: keeping
  `Resolution` on the root type.
- **Unsupported-media errors name what this provider takes**, not which
  other provider takes the file. A provider package does not know the
  others; the provider table in the docs does.
- **`Request.MarshalJSON` refuses media.** A request with media has no
  single JSON form on any provider, and an error is safer than a custom
  transport silently dropping files. Rejected: writing TypeSafe's
  base64 `assets` form, which would rewrite the state inside `MarshalJSON`.
- **A fixed 5-minute attempt timeout for media.** It is the timeout the
  CLI already uses, and one number is easy to explain. Rejected: a timeout
  that scales with file size, which is harder to predict and to document.
- **The log shows decide's request, with files summarized.** The
  `Transport` interface is frozen, so the client cannot see the provider's
  body without a new interface. Rejected: a new optional interface for
  logging.
- **Testing media for other providers uses an `httptest.Server`.**
  Attaching media is provider-independent, so `decidetest` covers most
  tests. For provider rules, the provider's own transport checks media
  before any network call, so a fake server proves both cases. Rejected:
  a fake per provider in `decidetest`.
- **`decidetest.PNG()` exists** because TypeSafe rejects images under 280
  pixels a side, so a test author cannot use a 1-pixel PNG.
- **`funnel.Shared` takes request options; `compact` does not take
  media.** Shared builds one request for many items, which can share one
  file. `compact` shortens text segments; a file has no segments to
  shorten, and a caller can still attach media to the requests that use
  the compacted text.
- **No `Provider.Takes(kind)` function.** UC-5 routes by kind with the
  documented table. Rejected for now: a support table in `backend`, which
  can come with the CLI track if it wants to drop its own.

## Success

- A Go caller attaches any file with `decide.WithMedia` and changes
  provider without changing request code.
- No provider sends a request that carries media it cannot read. A test
  per provider proves each supported kind reaches the wire in the right
  form, and each unsupported kind fails with `errors.ErrUnsupported` and
  no request.
- The CLI's media tests pass with no change in behavior.
- Must not get worse: requests without media are byte-for-byte the same
  on every provider; all checks in AGENTS.md pass.

## Risks and open questions

- **TypeSafe images are a preview.** The format, limits, or header can
  change. We keep the TypeSafe wire form in one place and say "preview" in
  the package comment. Not blocking.
- **Wrapping the state on TypeSafe changes paths.** A question that names
  `` `merchant` `` now finds it at `` `state.merchant` ``. Jev reads paths
  as text, and a question about the file can say "the attached photo".
  Docs show this. Not blocking.
- **Byte sniffing can reject a valid file** whose header decide misreads.
  We sniff only formats with fixed signatures, and accept unknown formats
  as declared. Not blocking.
- **OpenAI may support GIF in Decisions.** If OpenAI documents it, adding
  it is one line in the allow-list. Not blocking.
