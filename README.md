# Decide

**Ask typed questions about your data. Get answers with probabilities.**

Decide asks yes-or-no, multiple-choice, and scale questions about files,
folders, JSON records, and text. Each answer comes back typed, with a
probability, so a script or a program can act on it directly. There is no
prose to parse.

Use it as a command-line tool or as a Go library.

Decide runs on [System One](https://docs.typesafe.ai/introduction) models,
which are built to answer typed questions rather than to generate text:
TypeSafe's Jev and Cloudflare Workers AI's Clef.

## Try it

```sh
go install github.com/deepnoodle-ai/decide/cmd/decide@latest
export TYPESAFE_API_KEY=...   # from https://typesafe.ai

echo "The new release fixed everything I cared about" | decide run sentiment
```

```
Running sentiment on 1 line · typesafe jev-latest

stdin:1  The new release fixed everything I cared about
  sentiment  positive  94%

✓ 1 answered  nothing flagged  1.2s
Saved as run 20261002-153012-a1b2
See these results again with: decide runs view 20261002-153012-a1b2
```

Requires Go 1.27 or later. Run `decide` on its own for a tour.

## What you can ask

Decide comes with five skills. A skill is a named set of questions.

| Skill | Asks about each item |
| --- | --- |
| `sentiment` | Is it positive, negative, or neutral? |
| `ticket-routing` | Does this support ticket belong to billing, engineering, or other? |
| `relevance` | Is it relevant to a question you choose? |
| `code-risk` | Could it cause security or data problems, and how maintainable is it? |
| `receipt-quality` | Does this image show a readable receipt? |

```sh
decide run code-risk src --include '*.go'
decide run relevance docs --each section -p question="pricing"
decide runs view --json > results.jsonl
```

Decide reads JSONL, JSON, CSV, text, Markdown, source code, and images,
flags answers that need attention, and resumes stopped runs. You can write
your own skill in a few lines of JSON with `decide skills new`. The
[CLI guide](docs/cli.md) covers it all.

## Use it from Go

```sh
go get github.com/deepnoodle-ai/decide
```

```go
client, err := decide.NewClient() // reads TYPESAFE_API_KEY
if err != nil {
	return err
}
e, err := decide.Eval(ctx, client, ticket,
	decide.Noul("Is this about billing?"))
if err != nil {
	return err
}
fmt.Println(e.Answer.Noul) // the probability of yes, such as 0.97
```

Every answer is validated against its question, and transient failures
are retried. The [package documentation](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
covers asking several questions at once, `Pick`, and test fakes in
[`decidetest`](decidetest). Packages under `patterns/`, such as gate,
rank, and funnel, build common decisions from answers. The
[examples](examples) show each one in a short program.

## Providers

| Provider | Models | Credentials |
| --- | --- | --- |
| [TypeSafe](https://typesafe.ai) (default) | `jev-latest` | `TYPESAFE_API_KEY` |
| [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/models/clef/) | `clef`, `clef-flash`, with images | `CLOUDFLARE_AUTH_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` |

Choose one with `--provider` in the CLI or [`backend`](backend) in Go.
Image skills use Cloudflare automatically.

## Status

Decide is young. Before v1, the library API and the CLI may change in any
release.

## Contributing

Issues and pull requests are welcome. Please read
[CONTRIBUTING.md](CONTRIBUTING.md) first, and report security issues as
described in [SECURITY.md](SECURITY.md).

## License

[Apache 2.0](LICENSE)
