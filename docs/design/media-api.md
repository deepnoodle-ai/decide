# One media API for every provider

**Status:** Accepted
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
// MediaTransport is a Transport that can send Request.Media. The client
// sends a request with media only to a MediaTransport. It calls CheckMedia
// once per call, after Request.Validate and before the first attempt. An
// error from CheckMedia ends the call with no attempt. CheckMedia must not
// do I/O. A file the transport cannot send returns an error wrapping
// errors.ErrUnsupported. SystemOne must send every file in req.Media, or
// fail.
type MediaTransport interface {
	Transport
	CheckMedia(req *Request) error
}
```

`HTTPTransport` implements it, and `CheckMedia` rejects any file:
`typesafe does not take media yet (media[0], image/png); choose a provider
that reads images, such as backend.Gemini`. Its `SystemOne` calls
`CheckMedia` first, so it rejects media when used without the client.

`Request.MarshalJSON` is unchanged in code. Its doc gains one sentence:
"Media is not part of the native JSON form; a MediaTransport sends it."
`UnmarshalJSON` never sets `Media`.

### Shared validation: `internal/media`

A new package that imports only the standard library and not `decide`,
so the root package can import it without a cycle.

```go
package media

func Normalize(mimeType string) string            // lower case, no parameters, aliases resolved
func Kind(normalized string) string              // "image", "audio", "video", "pdf", or ""
func Sniff(data []byte) string                   // normalized type from fixed signatures, or ""
func Dimensions(mimeType string, data []byte) (w, h int, err error) // PNG, JPEG, GIF, WebP headers

type File struct {
	Index int    // position in Request.Media, for messages
	MIME  string // normalized
	Data  []byte
	URL   string
}

type Limits struct {
	Provider      string              // "cloudflare", in messages
	Formats       map[string][]string // kind → normalized MIME types taken as bytes
	URLKinds      []string            // kinds taken by URL; nil: no URLs
	RejectURL     func(u *url.URL) string // extra URL rule, such as YouTube; "" accepts
	MaxFiles      int
	MaxFileBytes  map[string]int64    // per kind, decoded bytes; 0: no limit
	MaxPixels     int                 // per image; 0: no limit
	MaxTotalBytes int64               // decoded, all inline files
	MaxBase64     func(files []File) int64 // inline base64 budget, such as Gemini's PDF rule
	Takes         string              // "PNG, JPEG, and WebP images, as bytes or by URL"
}

func (l Limits) Check(files []File) error // wraps errors.ErrUnsupported
```

The root package uses `Normalize`, `Kind`, and `Sniff` in
`Request.Validate` (PRD R-1). Each provider package declares one
`Limits` value and calls `Check` from its `CheckMedia`. `Dimensions`
moves Cloudflare's WebP chunk parser here and uses
`image.DecodeConfig` for PNG, JPEG, and GIF. It reads headers only.

Messages follow two forms, built in one place:

- `<provider> does not take <kind or form> (media[i], <mime>); it takes <Takes>`
- `<provider> takes <kind>s of at most <limit> (media[i] is <value>, <mime>)`

`Check` stops at the first failing file, in order: form (URL), kind,
format, per-file size, pixels, then count and totals.

The root package imports `internal/media`, which imports only the
standard library, so the root package's dependencies stay the standard
library. The maintainer approved this, and AGENTS.md's Layout section
says so.

### Root `Request.Validate`

Adds, after the existing checks:

1. Each `Media`: `MIMEType` set and of a known kind after `Normalize`;
   exactly one of `Data` and `URL`; `Data` not empty; `URL` absolute,
   `https`, with a host; `Sniff(Data)` empty or equal to the normalized
   type (with `image/jpeg` and `image/jpg` as one).
2. Reserved `Extra` keys: `images`, `media`, `videos`, `assets`.
3. `checkBytes` becomes a walk with `reflect` over maps, slices, arrays,
   pointers, interfaces, and exported struct fields, with a depth bound of
   64 and a visited set for pointers. `json.RawMessage` is skipped. The
   error names the path, such as `state.receipt` or `state.items[2]`.

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
`data:<mime>;base64,<data>`; a URL is sent as given. The `Extra` merge
stops decoding the body into a map: the transport writes the body's own
members, then each `Extra` member in key order whose key the body does
not use. Limits: PNG, JPEG, WebP; images by URL; 1,500 files; 512 MB
body; no other kind.

**Gemini.** With media, `input` is a list: a `text` block with the state
and a newline, then one block per file: `{"type":"image"|"audio"|"video"|"document","mime_type":...,"data"|"uri":...}`.
The `Extra` merge changes as for OpenAI. Limits: the MIME allow-lists in
`gemini/media.go`, with `video/mov` added for the `video/quicktime`
alias; every kind by bytes or URL; YouTube hosts rejected with a host
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
- `NewClientFor` without it is unchanged: the client uses
  `decide.HTTPTransport`, which rejects media as TypeSafe does.
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
  `Sniff` on each signature and on short input; `Dimensions` on PNG,
  JPEG, GIF, WebP VP8, VP8L, VP8X; `Check` messages for each limit; a fuzz
  test for `Dimensions` (moved from Cloudflare).
- Root: `Validate` cases for each R-1 rule, each reserved key, and deep
  `[]byte` in maps, slices, pointers, and structs; `WithMedia` appends;
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
  media is byte-for-byte the same as before (golden bodies recorded at
  the PRD's base commit); the allocation test.
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
9. `docs: media in the Go guides` — README, `start/go.mdx`,
   `tutorials/decide-from-go.mdx`, package comments, `examples/media`,
   roadmap "Media by URL", CHANGELOG.

## Rollout

Before v1 any API may change, and nobody uses the library yet. No shims.
The CHANGELOG lists the removed helpers and the new API. The CLI ships
with the same behavior apart from the two moves named above.

## Open questions

None. The maintainer approved the root package importing
`internal/media`, because that package imports only the standard
library. AGENTS.md's Layout section records it.
