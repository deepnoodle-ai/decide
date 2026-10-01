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
