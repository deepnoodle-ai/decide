# Sod

Go tools for System One Decisions.

- Module: `github.com/deepnoodle-ai/sod`
- Root package: `sod`

Requires Go 1.27 or later.

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
