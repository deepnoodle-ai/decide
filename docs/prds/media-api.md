# Attach a file the same way on every provider

Status: Draft
PRD PR: #107  Implementation PR: TBD  Updated: 2026-11-12

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
nested in state is sent as base64 text. `decidetest` fakes only the
TypeSafe API, and a request with media to it gets default answers with no
error. Go programs cannot ask a built-in template which kind of file it
reads. `funnel.Shared` cannot carry media.

**What the providers take** (public docs, read 2026-11-12):

- **TypeSafe (Jev).** PNG and JPEG images, as a preview. Each request must
  send the header `TypeSafe-Danger-Zone-Images: true`, and the model reads
  an image where its reference string appears in the state. No audio,
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
  format. A template's input is a media kind, or `""` for text.

Media does not change questions or answers. A question about media reads
it as part of the item, as it reads the state.

## Proposed API

Root package `decide` (standard library only):

```go
// MediaKind is the kind of file a request carries. "" means text.
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

// MediaTransport is a Transport that can send Request.Media. The client
// sends a request with media only to a MediaTransport, and calls
// CheckMedia once before the first attempt.
type MediaTransport interface {
	Transport
	CheckMedia(req *Request) error
}
```

Other packages:

```go
// decidetest: AcceptMedia makes the server's clients accept every file
// and record it in Recorded.Request.Media, as a provider that takes
// media would. Without it, a client rejects media as TypeSafe does.
func AcceptMedia() Option

// templates: Input returns the kind of file the template reads, or ""
// when it reads text.
func (t *Template) Input() decide.MediaKind

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

A full-size phone photo can be too large for one provider and fine for
another. Then the error says which limit it breaks:

```
unsupported operation: cloudflare takes images of at most 4 MiB (media[0] is 5.2 MiB, image/jpeg)
```

Acceptance:
- [ ] The same `Eval` call sends the photo on Cloudflare, OpenAI, and
      Gemini, each in its own wire form (see the provider table). A test
      per provider checks the body a fake server receives.
- [ ] On TypeSafe, the same call returns an error that matches
      `errors.Is(err, errors.ErrUnsupported)` and sends nothing (UC-2).
- [ ] A 4032x3024 JPEG of more than 4 MiB: OpenAI and Gemini send it.
      Cloudflare returns an error that matches `errors.ErrUnsupported`,
      not `decide.ErrInvalidRequest`, and names the provider, the limit,
      and the file's value, as above. No request reaches the fake server.
- [ ] `decide.Pick` takes `WithMedia` the same way.
- [ ] `Request.Media` is nil when no media is attached, and a request
      without media is sent exactly as today on every provider.
- [ ] The Go guide says that Cloudflare takes images of at most 4 MiB and
      16 megapixels, 8 MiB in all, so a caller must resize phone photos
      for it, and that TypeSafe takes no media yet.

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
	// This provider cannot read this file. Nothing was sent.
}
```

On Gemini, the call returns an answer. On OpenAI, it returns at once:

```
unsupported operation: openai does not take PDFs (media[0], application/pdf); it takes PNG, JPEG, and WebP images, as bytes or by URL
```

On TypeSafe, the default provider:

```
unsupported operation: typesafe does not take media yet (media[0], application/pdf); choose a provider that reads PDFs, such as backend.Gemini
```

Acceptance:
- [ ] On OpenAI and TypeSafe, the error matches
      `errors.Is(err, errors.ErrUnsupported)`, names the provider, the
      kind, the file's position and MIME type, and what the provider
      takes.
- [ ] A fake server records no request: the client makes no attempt and
      no retry.
- [ ] A file that is wrong on every provider (no MIME type, both `Data`
      and `URL`, an `http://` URL, PNG bytes declared as JPEG) matches
      `decide.ErrInvalidRequest`. A file one provider cannot take, by
      kind, format, form, size, pixel count, or count, matches
      `errors.ErrUnsupported` and not `ErrInvalidRequest`. So a caller can
      tell "fix the request" from "use another provider".

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
- [ ] `http://` and `gs://` URLs fail on every provider with
      `ErrInvalidRequest`. YouTube URLs (`youtube.com`, any
      `*.youtube.com`, `youtu.be`) fail on Gemini with `ErrUnsupported`.
- [ ] On TypeSafe and Cloudflare, any URL fails with `ErrUnsupported`:
      "cloudflare does not take media by URL (media[0]); attach the
      file's bytes in Data". On OpenAI, a video URL fails as a video, and
      an image URL is sent.

### UC-4: Test that my code attached one PNG

The test author's code is written for a provider that takes images. They
opt in to media on the fake server:

```go
func TestReceiptCheck(t *testing.T) {
	srv := decidetest.NewServer(t, decidetest.AcceptMedia())
	client := srv.Client(t)
	photo, err := os.ReadFile("testdata/receipt.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkReceipt(t.Context(), client, photo); err != nil {
		t.Fatal(err)
	}
	media := srv.Requests()[0].Request.Media
	if len(media) != 1 || media[0].MIMEType != "image/png" {
		t.Fatalf("media = %v, want one PNG", media)
	}
}
```

Without `AcceptMedia`, the same test fails with the TypeSafe error from
UC-2. That is the answer the default provider gives in production.

Acceptance:
- [ ] By default, a client from `decidetest` rejects every file with the
      same error as the TypeSafe transport, and the server records no
      request.
- [ ] With `AcceptMedia`, `Recorded.Request.Media` holds each file in
      order, with its MIME type and bytes or URL. `Recorded.Request.State`
      and `Recorded.Request.Extra` are what the caller built.
- [ ] With `AcceptMedia`, a file that is wrong on every provider still
      fails with `ErrInvalidRequest`. No provider's formats or limits are
      checked; the `AcceptMedia` doc says so.
- [ ] Faults, latency, retries, and canned answers work the same with
      and without media.
- [ ] Docs show how to test one provider's rules: point
      `backend.Config.BaseURL` at an `httptest.Server`. Unsupported media
      never reaches it; supported media arrives in the provider's wire
      form. This works for Cloudflare, OpenAI, and Gemini.

### UC-5: Route the built-in templates that read media

```go
for _, t := range templates.All() {
	switch t.Input() {
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
- [ ] `templates.ReceiptQuality.Input()` is `decide.MediaImage`,
      `templates.InvoiceCheck.Input()` is `decide.MediaPDF`, and every
      text template returns `""`. The doc comment says `""` means text.
- [ ] The `templates` package comment names `decide.WithMedia`, not
      provider helpers.

## Provider table

What each provider does with each kind. "Rejects" means the call returns
an error wrapping `errors.ErrUnsupported` before any network call.

| | TypeSafe (Jev) | Cloudflare (Clef) | OpenAI (GPT-6 Luna) | Gemini (Decision 1) |
|---|---|---|---|---|
| Image, bytes | Rejects, for now (#106). | PNG, JPEG, WebP. | PNG, JPEG, WebP. | PNG, JPEG, WebP, HEIC, HEIF, GIF, BMP, TIFF. |
| Image, URL | Rejects. | Rejects. | Sends the URL. | Sends the URL. |
| PDF | Rejects. | Rejects. | Rejects. | Bytes or URL. |
| Audio | Rejects. | Rejects. | Rejects. | Bytes or URL. |
| Video | Rejects. | Rejects. | Rejects. | Bytes or URL; no YouTube. |
| Other formats of a taken kind | n/a | Rejects. | Rejects (such as GIF). | Rejects a type not listed. |
| Where files go | n/a | Images before the state, as Clef places them. | One user message: the state as text, then one image part per file. | The state as a text block, then one block per file. |
| Limits checked before sending; a broken one rejects | n/a | 4 images; 4 MiB and 16 megapixels each; 8 MiB in all; 13 MiB body. | 1,500 images; 512 MB body. | 100 MB of inline base64; 50 MB when any PDF is inline. |

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
  `Data` is set and empty; `URL` is not an absolute `https` URL with a
  host; or the bytes contradict the declared type. These checks fail on
  every provider. Messages name the file as `media[i]`.
- R-2: Each provider rejects, before any network call, a file it cannot
  take, with an error wrapping `errors.ErrUnsupported`. That covers a
  kind, a format, a form (URL), and a limit of that provider: file size,
  pixel count, file count, total size. The message names the provider,
  the file, and either what the provider takes or the limit and the
  file's value:
  `cloudflare takes images of at most 16 megapixels (media[1] is 4096x4096, image/png)`.
  No transport forwards media it cannot send.
- R-3: `Request.Extra` keys `images`, `media`, `videos`, and `assets` are
  reserved. `Request.Validate` rejects them:
  `Extra key "images" is reserved; attach files with decide.WithMedia`.
  Other keys pass through as today.
- R-4: The client sends a request with media only to a transport that
  implements `MediaTransport`. For any other transport, built-in or
  custom, `SystemOne` returns an error wrapping `errors.ErrUnsupported`
  and calls no transport. For a `MediaTransport`, the client calls
  `CheckMedia` once, after `Request.Validate` and before the first
  attempt; an error from it returns at once, with no attempt and no
  retry. Each built-in transport's `SystemOne` also runs its own check,
  so a transport used without the client still rejects.
- R-5: `Request.MarshalJSON` writes no media. It is decide's native JSON
  form, which has no media, and its doc says so. The client encodes the
  request with `MarshalJSON` before the first attempt, as today, to catch
  encode errors and to log; this works for a request with media and
  copies no file bytes. Each transport reads `req.Media` and writes the
  files in its own wire form. No path fails before the transport runs
  because a request has media.
- R-6: A request with media gets a 5-minute attempt timeout by default.
  `WithAttemptTimeout` and `WithCallAttemptTimeout`, when given, win. A
  request without media keeps the 10-second default. An attempt of a
  request with media that ends by its own attempt timeout is not
  retried; the call returns that error. Other retryable failures (429,
  5xx, a reset connection) are retried as today.
- R-7: With `WithLogBodies`, the request body in the log has no file
  bytes. A `media` attribute lists each file by `Media.String()`. The log
  doc says the body is decide's request, not the provider's wire body.
- R-8: One attempt allocates at most 2 times the file's size on
  Cloudflare, OpenAI, and Gemini, for a 30 MiB file (base64 is 1.34
  times). A benchmark in each package checks it. The client copies no
  file bytes.
- R-9: `Request.Validate` rejects a `[]byte` at any depth of the state,
  instructions, and criteria, through maps, slices, arrays, pointers, and
  struct fields. `json.RawMessage` is allowed. The message names the path
  and the media API:
  `state.receipt is []byte, which would be sent as base64 text; attach files with decide.WithMedia, or pass text as a string`.
- R-10: `decidetest` without `AcceptMedia` builds clients with the
  TypeSafe transport, so a request with media fails as it does on
  TypeSafe. With `AcceptMedia`, the server's clients use a transport that
  implements `MediaTransport`, accepts every file that passes
  `Request.Validate`, and sends the files to the fake server, which
  records them in `Recorded.Request.Media`.
- R-11: The CLI attaches media with `decide.Media` and keeps its current
  behavior: the same providers per template, the same file limits, and
  the same 5-minute attempt timeout, now from the library. Only the lines
  that attach media and set the media timeout change.
- R-12: Docs in the same change: README "Use it from Go", the site's
  `start/go.mdx` and `tutorials/decide-from-go.mdx`, the root and
  provider package comments, the roadmap "Media by URL" line, and
  CHANGELOG. The Go guide shows the provider table, says which providers
  need smaller images, says that a media request has a 5-minute attempt
  timeout, and shows how to bound the whole call with a context deadline
  and `WithMaxRetries` (or `WithCallMaxRetries` on `SystemOne`). An
  `ExampleWithMedia` test and an `examples/media` program run against
  `decidetest` with `AcceptMedia`.

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
- The CLI's own media timeout constant, replaced by R-6.

## Not in scope

- **Images on TypeSafe Jev** (#106). Jev images are a preview behind the
  `TypeSafe-Danger-Zone-Images` header, and decide would have to place
  each image reference in the state. No one has measured how that
  placement changes the answers. The first step is a spike in `spikes/`
  that runs the built-in image templates on TypeSafe with the state
  wrapped and not wrapped, and records the answer change. It needs live
  calls and a key.
- Media inside question instructions or criteria. TypeSafe and OpenAI take
  it; Gemini and Clef do not. It is a later, separate feature.
- A Files API upload helper. Callers upload with Google's SDK and pass the
  URI in `URL`.
- Per-file quality settings (Gemini `resolution`, OpenAI `detail`). The
  defaults serve the use cases; see Decisions.
- A `decidetest` fake for each provider. See Decisions.
- TypeSafe Box questions.
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
- Media by URL only where the API takes it; no Files API helper; no PDF or
  GIF on OpenAI unless OpenAI documents it.
- The CLI moves with the smallest diff.
- **Shared media checks live in `internal/media`.** The root package and
  the provider packages import it. It imports only the standard library,
  so the root package's dependencies stay the standard library.
  AGENTS.md says so in its Layout section.
- **A limit of one provider is `ErrUnsupported`; a check that fails on
  every provider is `ErrInvalidRequest`.** A full-size phone photo is a
  valid request on Gemini. The caller must be able to tell "fix the
  request" from "use another provider". The message names the provider,
  the limit, and the value.
- **TypeSafe takes no media in this track.** Its transport rejects every
  kind with `errors.ErrUnsupported`. Jev images are a preview behind a
  header; sending them needs a state shape that no one has measured; the
  spike needs live calls and a key; and the CLI already keeps image
  templates off TypeSafe. Rejected: a multipart upload with the state
  wrapped as `{"state","media"}`, which changes the state shape on one
  provider only. #106 tracks it.

Made in this PRD:

- **A transport opts in to media with `MediaTransport`.** The client
  sends media only to a transport that has `CheckMedia`. So no transport,
  built-in or custom, drops files without an error, and every provider
  check runs once, before the first attempt. The `Transport` interface is
  frozen, and its doc already says a new ability arrives as an optional
  interface. Rejected: `Request.MarshalJSON` refusing media, the earlier
  draft. It broke the client's own encode-and-log step, and it protected
  only a custom transport that calls `MarshalJSON`, not one that builds
  its body from the fields.
- **`Request.MarshalJSON` writes no media.** With the opt-in, an error
  there protects nothing more, and it would break callers who encode a
  request to log it, cache it, or count its size (`patterns/compact`
  does). The client keeps one encode path for every request. Rejected:
  writing the files as base64 in the JSON, which has no meaning on the
  native API and copies every file.
- **`decidetest` rejects media by default and records it with
  `AcceptMedia`.** The default client is the TypeSafe transport, so a
  test cannot pass when production on the default provider fails. Code
  written for a provider that takes media opts in, and the test can check
  the files. Rejected: accepting media by default, which hides the
  production failure; a fake per provider, which copies each provider's
  rules into a second place.
- **`AcceptMedia` checks no provider's limits.** Its job is to show what
  the code attached. Provider rules are tested against the provider's own
  transport and an `httptest.Server`, through `backend.Config.BaseURL`,
  which every provider takes.
- **A media attempt that times out is not retried.** One attempt may take
  5 minutes. When a connection stops, three such attempts hold the call
  for more than 15 minutes and upload the file three times. A file that
  did not arrive in 5 minutes rarely arrives on the next try. Fast
  failures, such as 429, 503, and a reset connection, are still retried,
  because each costs one more upload, not 5 more minutes. decide cannot
  tell an upload from a wait for the answer without tracing the
  connection, so the rule covers the whole attempt. Rejected: default
  retries with the 5-minute timeout, which the reviewer accepted with
  docs; it keeps the worst case at 15 minutes.
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
  others; the provider table in the docs does. TypeSafe, which takes no
  media, names Gemini as an example.
- **A fixed 5-minute attempt timeout for media.** It is the timeout the
  CLI already uses, and one number is easy to explain. Rejected: a timeout
  that scales with file size, which is harder to predict and to document.
- **The log shows decide's request, with files summarized.** The client
  cannot see the provider's body without a new interface. Rejected: a new
  optional interface for logging.
- **`Template.Input()` returns a `decide.MediaKind`.** Template files and
  the CLI docs call this field `input`, and "media" in this PRD is a
  file. Rejected: `Template.Media()`, which used one word for two things.
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
  form, and each unsupported kind or broken limit fails with
  `errors.ErrUnsupported` and no request.
- The CLI's media tests pass with no change in behavior.
- Must not get worse: requests without media are byte-for-byte the same
  on every provider; all checks in AGENTS.md pass.

## Risks and open questions

- **The default provider takes no media.** A Go caller who starts on
  TypeSafe gets an error on the first file. The error names a provider
  that reads the file, and the Go guide shows the table. #106 adds images
  on Jev. Not blocking.
- **Byte sniffing can reject a valid file** whose header decide misreads.
  We sniff only formats with fixed signatures, and accept unknown formats
  as declared. Not blocking.
- **No retry after a media attempt times out** can fail a call that a
  second attempt would finish. The caller can retry, or raise the
  timeout with `WithAttemptTimeout`. Not blocking.
- **OpenAI may support GIF in Decisions.** If OpenAI documents it, adding
  it is one line in the allow-list. Not blocking.
