# One media API for every provider

**Status:** In Review
**Date:** 2026-11-12
**Workflow:** Spec, then build in parallel. The PRD settles the behavior;
this spec settles where the code goes. The build starts on a branch
stacked on the PRD branch.
**PRD:** [docs/prds/media-api.md](../prds/media-api.md)

## Context

Media rides in `Request.Extra` today, under three keys with three shapes
written by `cloudflare.SetImages`, `openai.SetImages`, and
`gemini.SetMedia`. A transport that does not know a key forwards it as an
unknown JSON field, so a wrong provider answers from the text alone. Each
provider validates in its own way, the client encodes the whole request
with every file before the first attempt, and the OpenAI and Gemini
transports encode, decode, and encode again when `Extra` is set. The PRD
defines one root type, `decide.Media`, and the rules each provider
follows. This spec says how to build it.

## Goals

- `decide.Media`, `Request.Media`, `WithMedia`, and `MediaTransport` in
  the root package, which imports only the standard library and one
  internal package that does the same.
- One copy of the MIME, sniffing, and limit code, shared by the root
  package and the three provider packages.
- No path sends media to a transport that has not opted in. Every media
  check runs before the first attempt.
- One attempt with a 30 MiB file allocates at most 60 MiB on Cloudflare,
  OpenAI, and Gemini, measured by a test in each package.
- The CLI diff touches only `Run.attach`, `callOptions`, and the media
  timeout constant in `internal/runs`.

## Non-goals

- Images on TypeSafe Jev (#106).
- Streaming a file from an `io.Reader`, or a body shared across attempts.
- Provider support tables in `backend`.

## Proposal

### Layers

```
caller ── WithMedia ──▶ Request{State, Questions, Media, Extra}
                              │
Client.SystemOne              ▼
  1. req.Validate()           every-provider checks ─────────▶ ErrInvalidRequest
  2. transport.(MediaTransport) missing ─────────────────────▶ ErrUnsupported
  3. CheckMedia(&r)           this provider's kinds, forms,
                              formats, limits ───────────────▶ ErrUnsupported
  4. r.MarshalJSON()          native JSON without media: encode check + log
  5. withRetry(transport.SystemOne(&r))
                              each attempt builds its own body from r.Media
```

Steps 1 to 4 run once per call. Nothing in them copies file bytes.

### Types (root package)

`media.go` holds `MediaKind`, its four constants, `Media`, `Kind`,
`String`, and `WithMedia`. `Request` gains `Media []Media` between
`Questions` and `Extra`. `WithMedia` appends, so two options add two
files.

`Media.String` prints `image/png, 48213 bytes` or
`video/mp4, https://storage.example.com/clip.mp4` with the URL's query
cut, because a signed URL carries a token in its query.

`transport.go` adds:

```go
// MediaTransport is a Transport that checks Request.Media itself and sends
// the files it takes. The client sends a request with media only to a
// MediaTransport. It calls CheckMedia once per call, after
// Request.Validate and before the first attempt. An error from CheckMedia
// ends the call with no attempt. CheckMedia must not do I/O. A file the
// transport cannot send returns an error wrapping errors.ErrUnsupported.
// SystemOne must send every file in req.Media, or fail. Implementing
// MediaTransport does not mean a transport takes any file: HTTPTransport
// rejects every file.
type MediaTransport interface {
	Transport
	CheckMedia(req *Request) error
}
```

`HTTPTransport` implements it, and `CheckMedia` rejects any file:
`typesafe does not take media yet (media[0], <mime>); choose a provider
that reads <kind>, such as backend.Gemini`, where `<kind>` is the plural
of the first file's kind: `images`, `audio`, `videos`, or `PDFs`. For a
PDF it reads as the PRD's UC-2 message:
`typesafe does not take media yet (media[0], application/pdf); choose a
provider that reads PDFs, such as backend.Gemini`. Its `SystemOne` calls
`CheckMedia` first, so it rejects media when used without the client.

`Request.MarshalJSON` is unchanged in code. Its doc gains one sentence:
"Media is not part of the native JSON form; a MediaTransport sends it."
`UnmarshalJSON` never sets `Media`.

### Shared validation: `internal/media`

A new package that imports only the standard library and not `decide`,
so the root package can import it without a cycle.

```go
package media

func Normalize(mimeType string) string                       // lower case, no parameters, aliases resolved
func Kind(normalized string) string                         // "image", "audio", "video", "pdf", or ""
func Contradicts(t string, data []byte) (looksLike string, bad bool) // the bytes cannot be type t
func Dimensions(t string, data []byte) (w, h int, err error) // PNG, JPEG, GIF, WebP headers
func IsYouTube(u *url.URL) bool                            // youtube.com, *.youtube.com, youtu.be
func AppendBase64(buf *bytes.Buffer, data []byte)          // one grow, no intermediate string

type File struct {
	Index int    // position in Request.Media, for messages
	MIME  string // normalized
	Data  []byte
	URL   string
}

type Limits struct {
	Provider      string                  // "cloudflare", in messages
	Takes         string                  // "PNG, JPEG, and WebP images, as bytes"
	Types         []string                // normalized MIME types it takes
	URLKinds      []string                // kinds taken by URL; nil: no URLs
	CheckURL      func(u *url.URL) string // extra URL rule, such as YouTube; "" accepts
	MaxFiles      int
	MaxFileBytes  int64 // decoded bytes of each inline file
	MaxPixels     int64 // pixels of each image whose header decide reads
	MaxTotalBytes int64 // decoded bytes of all inline files
	MaxBase64     int64 // base64 bytes of all inline files
	MaxBase64PDF  int64 // MaxBase64 when any inline file is a PDF
	Decimal       bool  // print sizes in MB, not MiB
}

func (l Limits) Check(files []File) error                 // wraps errors.ErrUnsupported
func (l Limits) BodyTooLarge(size, limit int64) error     // wraps errors.ErrUnsupported
```

The root package uses `Normalize`, `Kind`, `Contradicts`, and
`Dimensions` in `Request.Validate` (PRD R-1). Each provider package
declares one `Limits` value and calls `Check` from its `CheckMedia`.
`Dimensions` moves Cloudflare's WebP chunk parser here and uses
`image.DecodeConfig` for PNG, JPEG, and GIF. It reads headers only.

`internal/mediatest` builds small media files for tests (PNG, JPEG, GIF,
WebP headers, PDF, WAV, and MP4 starts, and a 4032x3024 phone photo), so
no fixture is committed.

Messages follow two forms, built in one place:

- `<provider> does not take <kind or form> (media[i], <mime>); it takes <Takes>`
- `<provider> takes <kind>s of at most <limit> (media[i] is <value>, <mime>)`

`Check` stops at the first failing file, in order: kind, form (URL),
URL rule, format, per-file size, pixels; then count and totals. Kind
comes before form, so a video URL on OpenAI fails as a video.

The root package imports `internal/media`, which imports only the
standard library, so the root package's dependencies stay the standard
library. One copy of the tables serves the root package and the three
providers, so they cannot drift. AGENTS.md's Layout section and
CONTRIBUTING.md name this one exception. See the open question.

### Root `Request.Validate`

Adds, after the existing checks:

1. Each `Media`: `MIMEType` set and of a known kind after `Normalize`;
   exactly one of `Data` and `URL`; `Data` not empty; `URL` absolute,
   `https`, with a host; the bytes not `Contradicts` the type. A PNG,
   JPEG, GIF, or WebP type needs bytes that start with its own signature
   and a header that `Dimensions` reads. For any other type, bytes that
   start with a signature in the table below contradict it only when the
   declared type, after `Normalize`, is not one of that signature's types
   and none of those types is of its kind. So FLAC bytes declared as a
   PDF fail, and FLAC bytes declared as an audio type that no provider
   lists pass here and fail at the provider with `ErrUnsupported`. A
   container that holds several formats is one row, so its types do not
   contradict each other. Bytes with no signature in the table are taken
   as declared. The message names the type as the caller declared it.
2. Reserved `Extra` keys: `images`, `media`, `videos`, `assets`. The
   first three are the removed helpers' keys. `assets` is the TypeSafe
   API's image member (PRD R-3); reserving it keeps files off TypeSafe
   until #106 sends them.
3. `checkBytes` becomes a walk with `reflect` over maps, slices, arrays,
   pointers, interfaces, and the struct fields `encoding/json` writes,
   with a depth bound of 64 and a visited set for pointers, slices (keyed
   by data pointer and length), and maps. The set records the depth of
   each visit, so a cycle is walked once and a container first reached at
   the depth bound is walked again from a shorter path. A cycle is no
   error in the walk; `encoding/json` reports it, so `Validate` returns
   `ErrInvalidRequest` for a cyclic state. Like
   `encoding/json`, it enters an embedded struct or pointer to struct,
   exported or not, and names its fields as fields of the outer struct. It rejects only bytes that
   `encoding/json` would write as base64, so it skips a nil `[]byte`
   (encoded as `null`), a field tagged `json:"-"`, an unexported field,
   and any value whose type, or a pointer to it, implements
   `json.Marshaler` or `encoding.TextMarshaler`. `json.RawMessage` is
   one of those. The error names the path, such as `state.receipt` or
   `state.items[2]`. `Validate` runs it on the state, the instructions
   and criteria, and each `Extra` value, after the reserved-key checks;
   an `Extra` path starts with the quoted key, such as `Extra "photo"`.

Signatures that `Contradicts` reads, and the normalized types that each
one can be:

| Signature | Bytes start with | Types |
| --- | --- | --- |
| PNG | `89 50 4E 47 0D 0A 1A 0A` | `image/png`, `image/apng` |
| JPEG | `FF D8 FF` | `image/jpeg` |
| GIF | `GIF87a` or `GIF89a` | `image/gif` |
| WebP | `RIFF`, 4 bytes, `WEBP` | `image/webp` |
| PDF | `%PDF-` | `application/pdf` |
| WAV | `RIFF`, 4 bytes, `WAVE` | `audio/wav` |
| MP3 | `ID3`, or a frame sync `FF FB`, `FF F3`, or `FF F2` | `audio/mpeg`, `audio/mp3` |
| Ogg | `OggS` | `audio/ogg`, `audio/opus`, `video/ogg` |
| FLAC | `fLaC` | `audio/flac` |
| MP4 (ISO base media: MP4, MOV, M4A, 3GP, HEIF, AVIF) | `ftyp` at bytes 4 to 8 | `video/mp4`, `video/mov`, `video/3gpp`, `video/3gpp2`, `audio/m4a`, `audio/aac`, `image/heic`, `image/heif`, `image/avif` |
| WebM (Matroska, EBML) | `1A 45 DF A3` | `video/webm`, `audio/webm`, `video/x-matroska` |

`audio/mpeg` and `audio/mp3` are not aliases of each other: Gemini lists
both names, so `Normalize` keeps each, and the MP3 row holds both. So an
MP3 declared `audio/mpeg` or `audio/mp3`, a WebM declared `audio/webm` or
`video/webm`, and a MOV declared `video/quicktime` (normalized to
`video/mov`) all pass. A MOV that starts with an older atom, such as
`moov` or `mdat`, has no signature in the table and is taken as declared.

`Normalize` resolves these aliases, which cover every name Go's
`mime.TypeByExtension` returns on macOS (`/etc/apache2/mime.types`) and
Linux (`media-types`, `shared-mime-info`, `mailcap`) for the extensions
the CLI reads, and the names `http.DetectContentType` returns:
`image/jpg`; `audio/x-wav`, `audio/wave`, `audio/vnd.wave`;
`audio/x-m4a`, `audio/mp4`, `audio/mp4a-latm`; `audio/x-aac`;
`audio/x-flac`; `audio/x-aiff`; `application/ogg`; `video/quicktime`;
`video/x-m4v`; `video/x-msvideo`, `video/vnd.avi`; `video/x-ms-wmv`. A
Gemini test
declares each of those names, and this machine's table, for a file of
each extension.

All three wrap `ErrInvalidRequest`. They are cheap: no file bytes are
read past the first 512.

### Client send path (`client.go`, `retry.go`, `options.go`)

In `SystemOne`, after `req.Validate()` and the model default:

```go
if len(r.Media) > 0 {
	mt, ok := c.transport.(MediaTransport)
	if !ok {
		return nil, fmt.Errorf("%w: decide: transport %s does not send media (media[0], %s); it must implement decide.MediaTransport",
			errors.ErrUnsupported, describeTransport(c.transport), r.Media[0].MIMEType)
	}
	if err := mt.CheckMedia(&r); err != nil {
		return nil, err
	}
	if !c.attemptTimeoutSet && cc.attemptTimeout == nil {
		p.attemptTimeout = mediaAttemptTimeout // 5 * time.Minute
	}
	p.noRetryOnTimeout = true
}
body, err := r.MarshalJSON() // no media: catches encode errors, feeds the log
```

- `clientConfig` gains `timeoutSet`, set by `WithAttemptTimeout`.
  `Client` keeps it as `attemptTimeoutSet`.
- `retryPolicy` gains `noRetryOnTimeout`. In `withRetry`, after an
  attempt fails: when it is set, the caller's context is live, and
  `errors.Is(err, context.DeadlineExceeded)`, return the error with no
  retry.
- `logSystemOne` adds `slog.Any("media", ...)`, a list of
  `Media.String()`, when `r.Media` is non-empty. `request_body` is the
  `MarshalJSON` output, which has no files. The `WithLogBodies` doc says
  the body is decide's request, not the provider's wire body.

`Eval`, `Pick`, and `ModelsService` need no change: `Eval` and `Pick`
apply `RequestOption`s to the request they build.

### Provider transports

Each provider package:

- deletes its media helpers and the `Extra["images"|"media"|"videos"]`
  branches;
- declares `var limits = media.Limits{...}` from the PRD's table;
- adds `func (t *Transport) CheckMedia(req *decide.Request) error`, which
  builds `[]media.File` from `req.Media` (slices share the bytes) and
  calls `limits.Check`, plus any body-size check;
- calls `CheckMedia` from `SystemOne` after `req.Validate()`;
- builds the body in one `bytes.Buffer`, grown once to the computed size,
  and writes each file's base64 with `base64.NewEncoder` straight into
  it.

**Cloudflare.** Body: the native JSON fields as today, then
`"images":[{"content_type":"image/png","base64":"..."}]`. The transport
stops calling `json.Marshal(req)` on the whole request when media is
present: it writes `r.MarshalJSON()` of the request without media,
drops the closing brace, and appends the `images` member. The 13 MiB
body limit is checked from the computed size before the buffer is
built, and wraps `ErrUnsupported`. Limits: 4 files; PNG, JPEG, WebP;
4 MiB and 16,000,000 pixels each; 8 MiB in all; no URLs; no other kind.

**OpenAI.** With media, `input` is one user message: an `input_text`
part with the state, then one `input_image` part per file. Bytes become
`data:<mime>;base64,<data>`; a URL is sent as given. For a request with
media, the transport writes the body's own members, then each `Extra`
member in key order whose key the body does not use; it does not decode
the body into a map. A request without media keeps today's merge, so
its body stays byte-for-byte the same. Limits: PNG, JPEG, WebP; images by URL; 1,500 files; 512 MB
body; no other kind.

**Gemini.** With media, `input` is a list: a `text` block with the state
and a newline, then one block per file: `{"type":"image"|"audio"|"video"|"document","mime_type":...,"data"|"uri":...}`.
The `Extra` merge changes as for OpenAI. Limits: the MIME allow-lists of
the old `gemini/media.go`, which already hold `video/mov` for the
`video/quicktime` alias; every kind by bytes or URL; YouTube hosts rejected with a host
match (`youtube.com`, `*.youtube.com`, `youtu.be`), not a substring; 100
MB of inline base64, or 50 MB when any inline file is a PDF.

The `Resolution` field goes with `gemini.Media`.

### Per-attempt copies

`Transport.SystemOne` runs once per attempt, so each attempt builds one
body. For a file of size n, that body is about 1.34n, and `net/http`
reads it through a `bytes.Reader` without a copy. The client's
`MarshalJSON` and the log hold no file bytes. A test in each provider
package runs `testing.Benchmark` on `encodeRequest` with a 30 MiB file
and fails when `AllocedBytesPerOp` exceeds 2n. Keeping one body across
attempts would need a cache keyed by request in the transport; it saves
nothing on the first attempt, which is the common case, so it is out.

### `decidetest`

- `AcceptMedia() Option` sets `s.acceptMedia`.
- `NewClientFor` without it builds `decide.NewHTTPTransport` and wraps
  it in `rejectTransport`, whose `CheckMedia` returns the TypeSafe error
  followed by `(in a test, pass decidetest.AcceptMedia() to
  decidetest.NewServer to accept media)`. When `opts` hold a connection
  option, which `WithTransport` does not allow, it builds the client as
  before, on the plain TypeSafe transport, without the hint.
- With it, `NewClientFor` builds `decide.NewHTTPTransport` itself and
  passes `decide.WithTransport(mediaTransport{inner})`. `mediaTransport`
  implements `MediaTransport`: `CheckMedia` returns nil, and `SystemOne`
  sends a copy of the request with `Media` nil and the files under
  `Extra["decidetest_media"]` as `[{"mime_type","data","url"}]`. Because
  `WithTransport` cannot be combined with connection options, a caller
  who passes `WithAPIKey`, `WithBaseURL`, `WithHTTPClient`, or
  `WithUserAgent` in `opts` gets an error; the `AcceptMedia` doc says so.
- `serveHTTP`, after it decodes the body, moves
  `Extra["decidetest_media"]` into `Request.Media` and deletes the key,
  so `Recorded.Request` holds the caller's state, extra, and files.
  `Recorded.Body` keeps the raw bytes as sent.
- No server-side media rules. No `decidetest.PNG()`.

### `templates` and `patterns/funnel`

- `(*Template).Input() decide.MediaKind` maps the internal
  `template.Input`: `text` to `""`, the others to the constant of the
  same name. The package comment's helper names become
  `decide.WithMedia`.
- `funnel.Shared` gains `opts ...decide.RequestOption`, stored on the
  `Layout` and applied to each request it builds, after the state and
  questions.

### CLI call sites

Another track (`fix/cli-media-inputs`) edits `internal/source`,
`internal/cli`, and `internal/runs` at the same time. This change edits
only these lines in `internal/runs/runs.go`:

- `Run.attach` loses its provider switch:
  `req.Media = append(req.Media, decide.Media{MIMEType: contentType, Data: data}); return nil`.
  The method and its call site stay, so the other track's diff around
  them still applies.
- `MediaAttemptTimeout` and `callOptions` go; `ask` calls
  `client.SystemOne(ctx, req)`. `timeout_internal_test.go` goes.
- The `cloudflare`, `openai`, and `gemini` imports go.

`internal/cli/run.go` keeps `checkMediaProvider` and `mediaLimit`: they
are the CLI's own rules, and the provider check now also runs in the
library. Two behaviors move: a provider limit error now comes from
`SystemOne`, after the cache lookup, so a fully cached item no longer
checks the file; and a media attempt that times out is no longer
retried. The CLI's media tests check wire shapes, which do not change.

## Alternatives considered

- **`Request.MarshalJSON` refuses media** (the PRD draft). It protects a
  custom transport only if that transport calls `MarshalJSON`. It broke
  the client's encode-and-log step, and it breaks callers who encode a
  request for their own use. `MediaTransport` covers every transport.
- **Validation in each provider package, as today.** Three copies of
  the alias table and sniffers drift; the audit found three different
  WebP checks. A shared exported package (`decide/media`) was the other
  option; it adds public API for code no caller needs.
- **Validation inside `withRetry` or the transport only.** A provider
  check in `SystemOne` alone runs on every attempt and returns an error
  the retry loop must classify. `CheckMedia` runs once and ends the call.
- **A `decidetest` that accepts media by default.** It lets a test pass
  when production on the default provider fails.

## Tradeoffs and consequences

- One more exported interface, `MediaTransport`, that custom transport
  authors must learn to send media. Without it they get a clear error.
- The root package imports an internal package. AGENTS.md now names
  that exception.
- A media attempt that times out is not retried. A slow but working
  link that needs a second try now fails; the caller can raise the
  timeout.
- `decidetest` with `AcceptMedia` puts files in the request as base64
  JSON, so `Recorded.Body` is large for large files. Tests use small
  files.
- The CLI loses its pre-cache provider check for fully cached items.

## Test plan

- `internal/media`: table tests for `Normalize` aliases and case;
  `Contradicts` on each row of the signature table, with each of its
  types, and on unknown bytes; `Dimensions` on PNG,
  JPEG, GIF, WebP VP8, VP8L, VP8X; `Check` messages for each limit; a fuzz
  test for `Dimensions` (moved from Cloudflare).
- Root: `Validate` cases for each R-1 rule, including an MP3 that
  starts with a frame sync declared `audio/mpeg`, a WebM declared
  `audio/webm`, and an `ftypqt` MOV declared `video/quicktime`, which
  pass; each reserved key; a `[]byte` in an `Extra` value; deep
  `[]byte` in maps, slices, pointers, structs, and embedded structs,
  exported or not; a slice and a map that hold themselves, which return
  promptly; and a nil `[]byte`, a
  `json:"-"` field, and a type with a `[]byte` field and its own
  `MarshalJSON` or `MarshalText`, which pass; `WithMedia` appends;
  `MarshalJSON` of a request with media equals the request without it.
  Client tests with a fake transport: a non-`MediaTransport` gets no
  call; `CheckMedia` runs once and its error means zero attempts; the
  5-minute default and both overrides; a timed-out media attempt is not
  retried while a 503 is; the log has a `media` attribute and no base64.
  `HTTPTransport` rejects media with no request to an `httptest.Server`.
  `ExampleWithMedia` with `decidetest.AcceptMedia`.
- `decidetest`: without `AcceptMedia`, media fails and no request is
  recorded; with it, `Recorded.Request.Media`, `State`, and `Extra` match
  the caller's; faults and retries still work.
- Cloudflare, OpenAI, Gemini, each against an `httptest.Server`: each
  supported kind and form reaches the wire in the provider's shape; each
  unsupported kind, format, form, and limit returns `ErrUnsupported`
  with the PRD's message and zero requests; a 4032x3024 JPEG over 4 MiB
  passes on OpenAI and Gemini and fails on Cloudflare; a request without
  media is byte-for-byte the same as before (its code path does not
  change, and the existing transport tests check its body); the
  allocation test. OpenAI's 512 MB and Gemini's 100 MB limits are
  checked through `internal/media`'s tests with small limits, not with
  files of that size. The allocation tests skip under the race detector,
  which changes allocation counts.
- `backend`: one test per provider that `Config.BaseURL` pointed at an
  `httptest.Server` receives a media request.
- `templates`: `Input()` for every built-in. `funnel`: `Shared` with
  `WithMedia` carries the file on every request.
- CLI: the existing media tests, unchanged.
- No live tests are added in this track. The existing Gemini live tests
  move to `decide.WithMedia`.

## Order of commits

Each commit builds and passes the checks.

1. `feat(media): shared MIME, sniffing, and limit checks` —
   `internal/media` and its tests.
2. `feat: attach media with decide.WithMedia` — root types,
   `MediaTransport`, `Validate` R-1, the client gate, timeout, no retry
   on timeout, log summary, `HTTPTransport` rejects media, `decidetest`
   `AcceptMedia`, `ExampleWithMedia`.
3. `feat(cloudflare): send decide.Media` — `CheckMedia`, body builder,
   limits, tests. The old helpers stay one more commit for the CLI.
4. `feat(openai): send decide.Media`.
5. `feat(gemini): send decide.Media`.
6. `refactor(cli): attach files with decide.Media` — the `internal/runs`
   lines above.
7. `feat!: remove the provider media helpers` — delete the helpers,
   reserve the `Extra` keys, deep `[]byte` check.
8. `feat(templates): Template.Input; feat(funnel): request options on Shared`.
9. `docs: media in the Go guides` — README, `start/go.mdx`, the root and
   `backend` package comments (the provider table, limits, timeout, and
   how to bound a call), the examples README, the roadmap, CHANGELOG, and
   the `backend` BaseURL test.

## Rollout

Before v1 any API may change, and nobody uses the library yet. No shims.
The CHANGELOG lists the removed helpers and the new API. The CLI ships
with the same behavior apart from the two moves named above.

## Open questions

- **Root importing `internal/media`** (for the maintainer to confirm in
  this pull request). The build follows the import. If the maintainer
  rules against it, the root package keeps `Normalize`, `Kind`,
  `Contradicts`, and `Dimensions` unexported and `internal/media` copies
  them, with a test that the two signature and alias tables match. The
  move is one commit and does not change behavior.
