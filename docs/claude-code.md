# Use decide in Claude Code

The decide plugin gives Claude a second opinion. Once it's installed, it
works in the background, and it adds two skills you run yourself: one
audits a repository for security bugs, and one finds where a fixed bug
was repeated. This guide shows how to set it up and use each part. For
what each check asks, what it sends, and its options, see the
[plugin's README](../plugin/README.md).

## Set it up

You need Claude Code 2.1.287 or later, decide 0.3.0 or later, and a key
for a decision model.

1. Install decide and set your key:

   ```sh
   brew install deepnoodle-ai/tap/decide
   export TYPESAFE_API_KEY=...
   ```

   To use Workers AI instead, set `DECIDE_PROVIDER=cloudflare` and the
   Cloudflare variables in the [README](../README.md#try-it). Set them
   where Claude Code will see them, such as in your shell profile. Check
   that decide works:

   ```sh
   echo "I love it" | decide run sentiment
   ```

2. In Claude Code, add the marketplace and install the plugin:

   ```text
   /plugin marketplace add deepnoodle-ai/decide
   /plugin install decide@decide
   ```

3. Restart Claude Code. If it can't find `decide`, such as when you start
   it from an app rather than a terminal, set `decidePath` in `/plugin` to
   the full path from `which decide`.

To update later, run `brew upgrade decide`, then update the plugin:

```sh
claude plugin update decide@decide
```

Then restart Claude Code. To update on its own, turn on auto-update for
the marketplace in `/plugin`. The plugin needs the decide release with the
same version number or a later one.

## What happens on its own

You don't need to do anything for these:

- **Content from outside is checked.** After Claude reads a web page,
  search results, an MCP tool's result, or `gh`, `curl`, `wget`, or `http`
  output, decide checks whether it tries to take over an AI agent or hide
  text from you. If it does, Claude reads a warning beside the result, and
  you see a toast.
- **Severe commands are checked, in bypass mode only.** Before each shell
  command runs, decide checks whether it could do severe harm that's hard
  to undo, such as deleting a home folder or force-pushing a shared
  branch. If so, you choose whether it runs. Set `commands` to `deny` or
  `off` in `/plugin` to change this.
- **Claude can ask its own questions.** The judge tool,
  `mcp__decide__judge`, lets Claude ask typed questions about up to 200
  items, such as which of these issues are bugs.

The footer shows a count, such as `decide 12 checked, 1 flagged`, and
`/decide` shows the session's checks in a table. A ⚠ in the status line
means some actions went unchecked or a check is off; `/decide` says why.

## Audit for security bugs

Run `/decide:audit` from the repository you want to review:

```text
/decide:audit
/decide:audit internal/api cmd/server
```

Name paths to audit only part of the repository. Claude then:

1. Picks the server code, leaving out tests, generated and vendored code,
   and examples, and counts the functions. Over 5,000 functions, it asks
   you before going on.
2. Runs decide's `security` template on every function. In one request
   per function, it asks about SQL and command injection, SSRF, XSS, weak
   ciphers, hashes and random values, and TLS checks turned off.
3. Takes the 15 functions that score highest and reads each one, with its
   callers, in parallel.
4. Reports a table of findings, each **confirmed** or **suspected**, with
   the reason, and counts the ones it **dismissed**.

decide's scores show where to look; they don't prove a bug. A finding is
confirmed only after Claude reads the code. When it's done, ask Claude
to read the next 15, which costs no new requests, to write a failing test
for each finding, or to fix them.

The audit doesn't cover path traversal, authorization, or
deserialization, and it can miss a bug that spans several functions.

## Find the rest of a fixed bug

When you fix a bug, the same mistake is often somewhere else. Give
`/decide:hunt` the fix, as a commit or a pull request:

```text
/decide:hunt 87
/decide:hunt a1b2c3d
/decide:hunt
```

With no argument, it uses the last commit. Claude then:

1. Reads the fix and picks the one mistake most likely to be repeated.
2. Writes one yes-or-no question about that mistake.
3. Tests the question: it must flag the code before the fix and pass the
   code after it. If it doesn't, Claude rewrites it.
4. Asks the question of every function, and reads the top ten.
5. Reports which are the same bug and which aren't.

The question is saved as a template, such as `.decide/templates/bug-87`,
and Claude offers to commit it. Committed, it can check later changes for
the same bug, such as in CI:

```sh
git diff main | decide run bug-87 --each function --fail-on matched
```

## Use the results outside Claude

Each audit and hunt is a saved decide run. To read one again in your
terminal:

```sh
decide runs                        # list runs
decide runs view <run-id> --top 15 # flagged first, then nearest a flag
```

Answers are cached, so running an audit again asks only about functions
that changed. The background checks are saved separately:

```sh
DECIDE_HOME=~/.decide/agent decide runs
```

## What it sends

Each check sends the text it checks to the decision model's provider,
with your key: in bypass mode, each shell command; each outside result
the content check reads; and with an audit or a hunt, every function it
covers. Runs and answers stay in `~/.decide` on your disk. See
[What it sends and keeps](../plugin/README.md#what-it-sends-and-keeps).
