# Decide

**Ask typed questions about your data. Get answers with probabilities.**

Decide asks yes-or-no, multiple-choice, and scale questions about files,
folders, JSON records, and text. Each answer comes back typed, with a
probability, so a script or a program can act on it directly. There is no
prose to parse.

It works two ways:

- **`decide`, a command-line tool.** Point it at your data and pick a skill.
  Every answer is saved so you can view it again or resume a run.
- **A Go library.** Put typed questions into your own code, with validated
  answers, retries, and test fakes.

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
stdin:1  The new release fixed everything I cared about
  sentiment  positive  94%

✓ 1 answered  1.2s
Saved as run 20261002-153012-a1b2
See these results again with: decide runs view 20261002-153012-a1b2
```

Requires Go 1.27 or later. Run `decide` on its own for a tour of the
commands.

## What you can ask

Decide comes with five skills. A skill is a named set of questions.

| Skill | Asks about each item |
| --- | --- |
| `sentiment` | Is it positive, negative, or neutral? |
| `ticket-routing` | Does this support ticket belong to billing, engineering, or other? |
| `relevance` | How relevant is it to a question you choose? |
| `code-risk` | How risky and how maintainable is this file or function? |
| `receipt-quality` | Does this image show a readable receipt? |

```sh
# Which source files deserve a second look?
decide run code-risk src --include '*.go'

# Which docs, or which sections of them, talk about pricing?
decide run relevance docs --each section -p question="pricing"

# Route every ticket in an export, reading one field of each record.
decide run ticket-routing export.json --items data.tickets --field body

# See the latest results again, or as JSON lines.
decide runs view
decide runs view --json > results.jsonl
```

Decide reads JSONL, JSON, CSV, text, Markdown, source code, and images. It
works out what one item is, such as a record, a line, or a whole file, and
`--each` can change that to a Markdown section, a paragraph, or a function.
Answers that need attention are flagged in red, and runs stopped with
Ctrl-C pick up where they left off with `decide runs resume`.

**Write your own skill** in a few lines of JSON, and commit it with your
project so your team can run it too:

```sh
decide skills new support-triage --from ticket-routing --project
decide run support-triage tickets.jsonl --dry-run
```

The [CLI guide](docs/cli.md) covers skills, items, saved runs, and
providers in full.

## Use it from Go

```sh
go get github.com/deepnoodle-ai/decide
```

Ask one question with `Eval`:

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

Or ask several at once, each with a typed handle:

```go
req := decide.NewRequest(ticket)
billing := decide.Ask(req, "billing", decide.Noul("Is this about billing?"))
tone := decide.Ask(req, "tone", decide.Choice("What is the customer's tone?",
	decide.Option("calm"), decide.Option("frustrated"), decide.Option("angry")))

resp, err := client.SystemOne(ctx, req)
if err != nil {
	return err
}
b, _ := billing.From(resp) // *decide.NoulAnswer
t, _ := tone.From(resp)    // *decide.ChoiceAnswer
fmt.Println(b.Noul, t.Choice, t.Confidence)
```

The client checks every answer against its question before you see it,
retries transient failures with backoff, and returns errors you can match
with `errors.Is`. `Pick` chooses one of your own values, or abstains. The
[`decidetest`](decidetest) package provides a fake server, so your tests
need no network or API key. See the
[package documentation](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
for the full API.

### Patterns

Packages under `patterns/` build common decisions from answers. Each one
has a short, runnable [example](examples).

| Package | What it does |
| --- | --- |
| [`pick`](patterns/pick) | Select one of your candidates, or abstain. |
| [`gate`](patterns/gate) | Allow, review, or escalate under thresholds you set. |
| [`fanout`](patterns/fanout) | Run many requests at once, with results in order. |
| [`rank`](patterns/rank) | Order candidates and keep the best within a budget. |
| [`funnel`](patterns/funnel) | Screen items in stages, keeping the reasons for each drop. |
| [`heads`](patterns/heads) | Ask a selector and its follow-up questions in one request. |
| [`compact`](patterns/compact) | Keep the most useful context within a size budget. |
| [`calibrate`](patterns/calibrate) | Fit thresholds on labeled answers, offline. |

### Providers

| Provider | Models | Credentials |
| --- | --- | --- |
| [TypeSafe](https://typesafe.ai) (default) | `jev-latest` | `TYPESAFE_API_KEY` |
| [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/models/clef/) | `clef`, `clef-flash`, with images | `CLOUDFLARE_AUTH_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` |

In Go, [`backend.NewClient`](backend) chooses the provider, and the rest of
your code stays the same. In the CLI, use `--provider cloudflare`. Image
skills use Cloudflare automatically.

## Status

Decide is young. Before v1, the library API and the CLI may change in any
release. The root package uses only the Go standard library.

## Contributing

Issues and pull requests are welcome. Please read
[CONTRIBUTING.md](CONTRIBUTING.md) first, and report security issues as
described in [SECURITY.md](SECURITY.md).

## License

[Apache 2.0](LICENSE)
