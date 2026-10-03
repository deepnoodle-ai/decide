# decide for Claude Code

A Claude Code plugin that gets Claude a second opinion from
[decide](../README.md) at the moments its own judgment is most likely to
slip. Each check asks the decision model a typed question and acts only
when the answer is likely.

| Check | When | decide asks | When the answer is likely |
| --- | --- | --- | --- |
| Command | Before each shell command Claude runs with Bash or Monitor | Could it destroy work that is hard to get back, or expose secrets? (`command-risk`) | You choose whether it runs, even in a mode that would run it without asking. Claude reads why when it doesn't. |
| Content | After WebFetch, WebSearch, an MCP tool, or a `gh`, `curl`, `wget`, or `http` command | Does it try to take over an AI agent, or hide text from a person? (`prompt-injection`) | Claude reads a warning beside the result, and you see a toast. |
| Reply | When Claude's turn ends | Does the reply claim more than its tools showed? Did Claude change code without checking it? (`reply-check`) | A band above the prompt offers to ask Claude to recheck. |

Claude also gets a **judge tool**, `mcp__decide__judge`, for its own typed
questions over up to 200 items, and a **skill** that teaches it to run
decide and write templates.

## Install

You need Claude Code 2.1.287 or later, decide 0.2.0 or later, and a key for
a decision model:

```sh
brew install deepnoodle-ai/tap/decide
export TYPESAFE_API_KEY=...    # or the Cloudflare variables; see the decide README
```

Then, in Claude Code:

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

Run `/decide` to see what it checked in this session. Every check is also
saved as a decide run:

```sh
DECIDE_HOME=~/.decide/agent decide runs
```

To update the plugin, run `/plugin marketplace update decide`, or turn on
auto-update for the marketplace in `/plugin`.

## What it sends and keeps

These go to the decision model's provider, with your key:

- each shell command Claude runs;
- each result the content check reads, whole: web pages, search results,
  `gh` and `curl` output, and every MCP tool's result, private connectors
  included;
- each final reply, with the first 800 characters of each tool result in
  that turn, up to 12,000 characters in all. A tool result can be a file's
  contents, such as a `.env` file Claude read.

Each check is also saved on your disk as a decide run under
`~/.decide/agent/runs`, readable only by you. Nothing removes them; delete
the folder to clear them.

## What it is not

A second opinion, not a sandbox. The command check judges the text of a
command: `make clean` or a script can hide what it does. When decide cannot
answer (it is not installed, the key is missing, or the provider fails),
the action goes on. The check says why in the transcript, the status line
counts what went unchecked, and the check tries again a minute later.
`/decide` lists any check that is off, and why.

The plugin runs decide from `~/.decide/agent`, so a repository's
`.decide/templates` cannot replace a check's template. A template of the
same name in `~/.decide/agent/templates` or `~/.decide/agent/.decide/templates`
would, so the plugin turns that check off and says so.

## Options

Set them in the `/plugin` menu.

| Option | Default | Meaning |
| --- | --- | --- |
| `commands` | `ask` | Flagged commands: `ask` you, `deny` them, or `off` to not check commands |
| `decidePath` | `decide` | The decide command to run |

Each template's flags decide what is flagged. `decide templates show
command-risk` lists them.

## Developing

```sh
claude plugin validate plugin
claude plugin test plugin
claude --plugin-dir plugin
```

Claude Code writes the API's types to `plugin/.claude-plugin/types/` when
it loads the plugin from a folder you own, such as with `--plugin-dir`.
After that, `npx tsc -p plugin` type-checks it.
