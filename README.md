# Sod

Go tools for System One Decisions.

- Module: `github.com/deepnoodle-ai/sod`
- Root package: `sod`

Requires Go 1.27 or later.

## Client

Set `TYPESAFE_API_KEY` and create a client:

```go
client, err := sod.NewClient()
if err != nil {
	return err
}
req := sod.NewRequest("I was charged twice. Please help.")
billing := sod.Ask(req, "billing", sod.Noul("Is this about billing?"))
resp, err := client.SystemOne(ctx, req)
if err != nil {
	return err
}
answer, err := billing.From(resp)
if err != nil {
	return err
}
fmt.Println(resp.Model, answer.Noul)
```

Import `github.com/deepnoodle-ai/sod`. Requests support Noul, Choice, and
Score questions with typed answers. The client validates answers against
their questions and retries transient failures with backoff. Model names
are strings; responses expose the resolved model and request ID.

Client options configure authentication, base URL, model, logging, retry
behavior, and transport. Environment defaults use `TYPESAFE_API_KEY`,
`TYPESAFE_BASE_URL`, `TYPESAFE_DEFAULT_MODEL`, and `TYPESAFE_LOG_LEVEL`.

The `github.com/deepnoodle-ai/sod/sodtest` package supplies a fake HTTP
server, answer fixtures, request recording, and queued failures for tests.

## Choose a backend

The experimental `x/backend` package selects TypeSafe Jev or Cloudflare
Clef during client construction. The resulting client uses the same
requests, questions, typed handles, validation, and retry settings:

```go
import "github.com/deepnoodle-ai/sod/x/backend"

client, err := backend.NewClient(backend.Config{
	Provider:  backend.Cloudflare,
	APIKey:    os.Getenv("CLOUDFLARE_AUTH_TOKEN"),
	AccountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
	Model:     "clef", // use "clef-flash" for the faster model
})
if err != nil {
	return err
}
// Use client.SystemOne(ctx, req) and the same typed handles as above.
```

Change the configuration to switch services:

| Provider | APIKey | AccountID | Default model |
| --- | --- | --- | --- |
| `backend.TypeSafe` | TypeSafe API key | empty | `jev-latest` |
| `backend.Cloudflare` | Workers AI API token | Cloudflare account ID | `clef` |

`backend.NewClient` reads no environment variables itself. Applications
choose where to obtain configuration, as the example does with `os.Getenv`.
An existing `TYPESAFE_*` environment cannot affect this constructor.
`sod.NewClient()` retains its existing TypeSafe environment defaults.

Set connection settings through `backend.Config`. Pass shared client options
such as `sod.WithMaxRetries`, `sod.WithAttemptTimeout`, and `sod.WithLogger`
to `backend.NewClient`. `sod.WithRequestModel("clef-flash")` can select the
other Cloudflare model for a single request.

The experimental `x/cloudflare` package also exposes `NewTransport` for
use with `sod.WithTransport` and an explicit `sod.WithModel`. It handles the
Workers AI account-scoped routes, response envelope, and provider errors.
HTTP failures expose `*sod.APIError` through `errors.As` and existing error
sentinels. `cloudflare.Error` retains the full provider error array.
`Response.Raw` holds the redacted envelope, and `Response.Header` retains
headers such as `CF-Ray`. `Models.List` returns `errors.ErrUnsupported` for
this adapter.

Clef accepts Noul, Choice, and Score questions. The adapter checks its
documented request limits before sending: at most 64 questions, restricted
question IDs, 2–255 choice options, and 2–10 score levels. It preserves
returned probabilities and confidence values. Cloudflare documents that
long text state is truncated by the service to its context limit.

### Embedded images

Use `cloudflare.NewImage` and `cloudflare.SetImages` with Clef or Clef-flash:

```go
img, err := cloudflare.NewImage("image/png", pngBytes)
if err != nil {
	return err
}
req := sod.NewRequest("Review the attached receipt.")
receipt := sod.Ask(req, "receipt", sod.Noul("Is a receipt visible?"))
if err := cloudflare.SetImages(req, img); err != nil {
	return err
}
resp, err := client.SystemOne(ctx, req)
if err != nil {
	return err
}
answer, err := receipt.From(resp)
```

The hosted API accepts embedded PNG, JPEG, and WebP images. It allows four
images, 4 MiB and 16 megapixels per image, 8 MiB of combined decoded image
data, and a 13 MiB complete request. The helper checks image headers and
these limits; the provider validates the full image data. Base64 data URLs
also work through `Request.Extra["images"]`. Remote image URLs and hosted
video inputs are unsupported. Build requests before sharing them between
goroutines.

Schemas: [Clef](https://developers.cloudflare.com/workers-ai/models/clef/)
and [Clef-flash](https://developers.cloudflare.com/workers-ai/models/clef-flash/).
OpenAI Decisions support awaits a verified API contract. The reported
preview's request format and probability semantics are not yet established
by the documentation used for this integration.

## Development

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
```

CI also checks formatting and verifies that `go mod tidy` leaves the
module files unchanged.

## License

Apache 2.0. See [LICENSE](LICENSE).
