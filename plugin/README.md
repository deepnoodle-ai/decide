# decide for Claude Code

A Claude Code plugin that gets Claude a second opinion from
[decide](../README.md) at the moments its own judgment is most likely to
slip. Each check asks the decision model a typed question and acts only
when the answer is likely.

| Check | When | decide asks | When the answer is likely |
| --- | --- | --- | --- |
| Command | In bypass mode, before each shell command Claude runs with Bash or Monitor | Could it cause severe harm that is hard to undo, such as deleting a home folder, force-pushing a shared branch, dropping a database, or sending secrets away? (`command-risk`'s `severe`) | You choose whether it runs. Claude reads why when it doesn't. |
| Content | After WebFetch, WebSearch, an MCP tool, or a `gh`, `curl`, `wget`, or `http` command | Does it try to take over an AI agent, or hide text from a person? (`prompt-injection`) | Claude reads a warning beside the result, and you see a toast. |

Claude also gets a **judge tool**, `mcp__decide__judge`, for its own typed
questions over up to 200 items, and a **skill** that teaches it to run
decide and write templates.

**`/decide:hunt <commit or PR>`** finds the other places a fixed bug lives.
Claude writes one question about the mistake the fix corrected, checks
that it flags the code before the fix and passes the code after, asks it
of every function in the repository, and reads up to ten that score 30%
or more to confirm them. The question is saved as a project template, such as
`.decide/templates/bug-87`, so later changes can be checked for the same
bug.

**`/decide:audit [path]`** finds where to look for security bugs. decide's
`security` template ranks every function for SQL and command injection,
SSRF, XSS, and weak cryptography; Claude reads up to 15 that score 30% or
more, with their callers, in parallel, and reports which are real and
which it dismissed, and why.

For a walk through setup and each skill, see the
[user guide](https://decide.deepnoodle.ai/reference/claude-code/).

## Install

You need Claude Code 2.1.287 or later, decide 0.4.0 or later, and a key for
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

The footer beside the prompt shows how much decide checked and flagged,
such as `decide 12 checked, 1 flagged`. A checked command or result shows
`decide ✓` at the right of its row, or a line under it with what was
flagged. Run `/decide` to see the session's checks in a table. Every check
is also saved as a decide run:

```sh
DECIDE_HOME=~/.decide/agent decide runs
```

To update the plugin, run `claude plugin update decide@decide` and restart
Claude Code, or turn on auto-update for the marketplace in `/plugin`.

## What it sends and keeps

These go to the decision model's provider, with your key:

- in bypass mode, each shell command Claude runs;
- each result the content check reads, whole: web pages, search results,
  `gh` and `curl` output, and every MCP tool's result, private connectors
  included. A result over 1,000,000 characters goes on unchecked;
- with `/decide:audit` or `/decide:hunt`, every function in the
  repository it covers. Its runs and answers are kept in your own
  `~/.decide`, like any `decide run`.

Each check is also saved on your disk as a decide run under
`~/.decide/agent/runs`, readable only by you. Nothing removes them; delete
the folder to clear them.

Answers are kept in decide's answer cache, `~/.decide/agent/cache`, so a
command Claude runs again, such as `go test ./...`, is answered without a
request. The cache holds hashes and answers, not the commands or text.
It is keyed by the model name. When a live answer shows the model behind
it changed, older answers are asked again. A check whose answer is cached
can't see an upgrade; delete the folder to ask fresh.

## What it is not

A second opinion, not a sandbox. The command check runs only in bypass
mode. In the other modes, Claude Code asks you about each command you
haven't allowed, or its auto-mode classifier judges it. A command that
matches one of your allow rules runs unchecked, as you chose.
It flags only severe harm: discarding changes in your checkout, such as
`git checkout -- .`, is not flagged. It judges the text of a command, so
`make clean` or a script can hide what it does. When decide cannot
answer (it is not installed, the key is missing, or the provider fails),
the action goes on. The check says why in the transcript, the status line
shows how many actions went unchecked, and the check tries again a minute
later. The status line shows only then, so its ⚠ means something.
`/decide` lists any check that is off, and why.

The plugin runs decide from `~/.decide/agent`, so a repository's
`.decide/templates` cannot replace a check's template. A template of the
same name in `~/.decide/agent/templates` or `~/.decide/agent/.decide/templates`
would, so the plugin turns that check off and says so.

## Options

Set them in the `/plugin` menu.

| Option | Default | Meaning |
| --- | --- | --- |
| `commands` | `ask` | In bypass mode, severe commands: `ask` you, `deny` them, or `off` to not check commands |
| `decidePath` | `decide` | The decide command to run |

The command check flags `severe` at 80%, and the content check flags
`injection` or `hidden` at 60%, as the templates do. `decide templates
show command-risk` shows the questions.

## Developing

```sh
claude plugin validate plugin
claude plugin test plugin
claude --plugin-dir plugin
```

Claude Code writes the API's types to `plugin/.claude-plugin/types/` when
it loads the plugin from a folder you own, such as with `--plugin-dir`.
After that, `npx tsc -p plugin` type-checks it.
