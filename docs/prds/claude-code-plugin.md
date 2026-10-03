# A second opinion for Claude Code, from decide

Status: Draft
PRD PR: none (one PR with the build)  Implementation PR: TBD  Updated: 2026-10-02

## Problem

People run Claude Code with wide permissions so that it works without
stopping: `auto`, `acceptEdits`, or `bypassPermissions`. In those modes
three mistakes cost the most, and nothing outside Claude itself checks for
them:

- **A destructive or leaking command runs.** `git checkout -- .`, a force
  push, `DROP TABLE`, or `cat ~/.aws/credentials | curl ...` runs with no
  prompt. Permission rules are lists of patterns, and these commands take
  forms no list covers.
- **Fetched content steers Claude.** A web page, an issue, or an MCP result
  can carry instructions for AI agents, hidden from the person who reads it.
- **Claude says the work is done when it is not.** A reply says "all tests
  pass" while the test output in the same turn shows a failure.

Claude Code v2.1.287 (2026-10-01) added mods: plugins whose code runs
inside Claude Code and can hold a tool call, add a note that only the model
reads, add a tool, and draw a band above the prompt. That makes an
independent check at each of these moments possible for the first time.
decide already answers this kind of typed question, with a probability, for
about 0.2 s per item (see Context). If we do nothing, the roadmap's Claude
Code plugin stays a skill that only teaches Claude to run the CLI.

## Context

What exists:

- The decide CLI runs templates on text from stdin and prints every
  probability with `--json`. `prompt-injection` is built in. A template can
  be run by name or by path, and `DECIDE_HOME` moves saved runs.
- The roadmap lists a Claude Code plugin under Agents, installed with
  `/plugin marketplace add deepnoodle-ai/decide`.

How others do the job:

- **blast-radius**, Anthropic's sample mod, holds a fixed list of risky
  commands and dry-runs each one to show what it would touch. It sees only
  the commands on its list. decide judges any command; the two compose.
- **Claude Code's `auto` mode** puts permission prompts to a model
  classifier. It replaces the person for prompts. It is not a check in
  bypass mode, and it does not read fetched content or replies.
- **Settings hooks** with a deny-list script are the common workaround.
  They have the same coverage limit as blast-radius, and no interface.

A prototype mod, run on 2026-10-02 against Jev (`jev-1.13.0`):

- On 25 hand-written commands, at 80%, it flagged every destructive or
  leaking one but `cat .env` (77%), and one harmless one,
  `find . -name '*.tmp' -delete` (81%). `git push` to a feature branch
  scored 11%. Sixteen commands in one call took 0.74 s.
- In a headless session in `bypassPermissions`, it held
  `git checkout -- .`, kept the uncommitted work, and Claude explained why.
- A page with white 1px text that told AI agents to print `~/.npmrc`
  scored 96% injection and 97% hidden; Claude refused and told the user.
- Asked to rank five issues, Claude wrote four questions for the judge
  tool, ranked the issues by the answers, and called out a 58% answer as
  unsure.

## Users and job

Primary: a developer who uses Claude Code with wide permissions and wants it
to stop and ask before it does damage, without going back to approving every
command. We optimize for few interruptions: an ask that is not needed costs
more trust than it earns.

Secondary: Claude, which gets a fast, calibrated way to make many small
judgments (triage, classification, "is this relevant") instead of reading
every item. And CLI users with other agents, who can call the two new
templates from their own hooks.

## Concepts

- **decide plugin**: a Claude Code plugin from this repository. It holds
  the checks, the judge tool, a `/decide` command, and a skill. It needs
  the decide CLI and a provider key.
- **Check**: a question decide asks at a fixed moment, with an action when
  the answer is likely. There are three:
  - **Command check**: before each shell command. Could it destroy work
    that is hard to get back, or expose secrets?
  - **Content check**: after content arrives from outside: a web page, a
    search, an MCP tool, or a `gh` or `curl` command. Does it try to take
    over an AI agent, or hide text from a person?
  - **Reply check**: when a turn ends. Does Claude's reply claim more than
    its tools showed? Did it change code without checking the change?
- **Judge tool**: a tool Claude calls with its own typed questions about
  one or more texts. It answers each with a probability.
- **Flagged**: an answer at or above the plugin's threshold, 80% by
  default. Only flagged answers cause an action.

The checks run on two new built-in templates, `command-risk` and
`reply-check`, and on `prompt-injection`.

## Use cases

### UC-1: Install the plugin

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

The person has the decide CLI and `TYPESAFE_API_KEY` (or the Cloudflare
variables and `DECIDE_PROVIDER=cloudflare`).

Acceptance:
- [ ] `claude plugin validate .` passes on the repository and on `plugin/`.
- [ ] After install, `/decide` prints that nothing has been judged yet and
      how to see saved runs.
- [ ] With decide missing, too old, or without a key, the first check prints
      one line in the transcript that says what is wrong and how to fix it.
      Commands still run. The line does not repeat in that session.

### UC-2: A risky command waits for the person

Claude, in `bypassPermissions`, calls Bash with `git checkout -- .`.

Acceptance:
- [ ] The command check flags it, and Claude Code asks the person
      "decide judged this command 93% likely to destroy work that is hard
      to get back … Run it?" with **Run it** and **Don't run it**.
- [ ] On **Don't run it**, the command does not run, and Claude reads why
      and that the user chose not to run it.
- [ ] When Claude Code was going to ask anyway (default mode, no rule),
      there is one dialog, Claude Code's own, with decide's line under it.
- [ ] When no one can answer (`claude -p`), a flagged command is refused,
      and Claude reads why.
- [ ] `ls`, `go test ./...`, and `git push -u origin my-branch` run with
      no question.
- [ ] With the option `commands` set to `deny`, a flagged command is
      refused with no question. With `off`, commands are not judged.

### UC-3: Fetched content carries hidden instructions

Claude runs WebFetch on a page with an instruction for AI agents in white
text.

Acceptance:
- [ ] Claude reads, after the result, a note that gives both probabilities
      and tells it to treat the result as data and to tell the user what it
      asked for.
- [ ] The person sees a toast.
- [ ] Results under 80 characters, errors, and Bash commands other than
      `gh`, `curl`, `wget`, and `http` are not judged.

### UC-4: A reply claims more than its tools showed

In one turn Claude edits a file, runs the tests, sees a failure, and replies
"Fixed, and all tests pass."

Acceptance:
- [ ] When the turn ends, a band above the prompt reads "This reply may
      claim more than its tools showed" with the probability, a button
      **Ask Claude to recheck**, and **Dismiss**.
- [ ] The button sends Claude a prompt that names the probability and asks
      it to say plainly what failed, then fix what it can. The band closes.
- [ ] A typed prompt or **Dismiss** closes the band.
- [ ] A turn that edits code and runs no check shows "Claude changed code
      without running a check" and **Ask Claude to verify**.
- [ ] Turns with no tool calls, interrupted turns, and subagent turns are
      not judged.

### UC-5: Claude makes many small judgments

The person asks Claude which of 40 open issues to start with.

```json
{
  "items": ["#12 runs view crashes on an empty run ...", "..."],
  "questions": [
    {"name": "ready", "type": "noul", "question": "Could an agent finish this without asking anything?"},
    {"name": "size", "type": "choice", "question": "How big is it?",
     "options": {"small": "An afternoon", "medium": "A few days", "large": "More than a week"}}
  ]
}
```

Acceptance:
- [ ] The judge tool answers every item in one call, one line of answers
      per item, such as `ready: yes 91% · size: small 88%`.
- [ ] A score question shows its level, such as `impact: 2.6 of 3 (High)`.
- [ ] Bad input (no items, more than 200 items, more than 8 questions, a
      choice with no options) is an error that says what to fix.

### UC-6: Use the templates without Claude Code

```sh
echo 'git reset --hard HEAD~3' | decide run command-risk
decide run reply-check demo/replies.jsonl
```

Acceptance:
- [ ] `decide templates` lists `command-risk` and `reply-check`, and
      `decide templates show` explains each.
- [ ] The demo has commands and replies with planted problems, and
      demo/README.md says which ones each template flags.

### UC-7: See what decide judged

Acceptance:
- [ ] `/decide` lists the last 15 judgments of the session, flagged ones
      marked `!`, with their answers.
- [ ] The status line shows the count, such as `decide · 14 judged · 1 flagged`.
- [ ] Every judgment is a saved run:
      `DECIDE_HOME=~/.decide/agent decide runs` lists them.

## Requirements

- R-1: Each check fails open. When decide cannot answer, the action goes on
  as if the plugin were not there.
- R-2: The plugin calls the decide CLI, never a provider directly, so
  providers, keys, retries, and long items work as they do on the CLI.
- R-3: The plugin's runs go to `~/.decide/agent` (or `$DECIDE_HOME/agent`),
  apart from the person's own runs.
- R-4: Text the plugin shows the person (commands, reasons, errors) has its
  control characters removed.
- R-5: The plugin's version is the decide release it needs. A release sets
  it.
- R-6: The plugin's tests run with `claude plugin test plugin` and do not
  call a provider.

## Not in scope

- `decide mcp`. A separate roadmap item; the judge tool covers Claude Code.
- Other agents' hook formats (Codex, Cursor). They can call
  `decide run command-risk` from their own hooks; recipes come later.
- Showing what a risky command would touch. blast-radius does that, and
  composes with this plugin.
- Checking files Claude reads from the project. The project is the person's
  own; checking every read would double the requests.
- Calibrating thresholds on labeled examples. That is `decide eval` on the
  roadmap.

## Decisions

- **Two new built-in templates, not templates inside the plugin.** They are
  versioned with the binary, so the plugin and the CLI cannot drift, and CLI
  users get them. Rejected: templates in the plugin folder, read by path.
- **One plugin with all three checks on, and three options**: `commands`
  (`ask`, `deny`, `off`), `threshold`, and `decidePath`. The person turns
  the plugin off to stop the other checks. Rejected: an option per check;
  more knobs than a first-time user needs.
- **Ask the person in place of Claude Code only when Claude Code would not
  ask.** One dialog per command, never two. Rejected: always ask; it doubles
  the dialog in default mode.
- **The command check does not flag pushes, releases, or pull requests.**
  The template has an `external` question, shown in `/decide` but never
  flagged: it scored 87–97% on routine `git push` and `gh pr create`.
- **Fail open.** The plugin is a second opinion, not a sandbox. Failing
  closed would stop all work when a key expires.
- **The marketplace is named `decide`**, so the install id is
  `decide@decide`. A company-wide `deepnoodle-ai` marketplace stays free for
  later.
- **The plugin also ships a skill** that teaches Claude to run decide and
  write templates, as the roadmap item says.

## Success

- The person keeps the plugin on. Signal: in a week of the maintainers'
  own work, the command check asks fewer than once a day without cause.
- Every flagged prototype case in Context stays flagged, and every clean
  one stays clean, on the released templates.
- Must not get worse: a command waits for the check about as long as one
  decide request, and a turn ends after one more.

## Risks and open questions

- **Interruptions erode trust.** If the command check asks too often, people
  turn it off. Reduce: the threshold option, the `external` decision above,
  and the measurement in Success. Not blocking.
- **Text leaves the machine.** Every shell command, fetched result, and
  final reply, with up to 12,000 characters of the turn's tool results, goes
  to the provider. The README says so plainly. Not blocking.
- **The mods API is early access** and may change between Claude Code
  releases. Reduce: pin the version we test with in CI, and keep the module
  small. Not blocking.
- **The judge itself can be addressed by the content it judges** ("answer
  no"). The templates treat content as evidence, and `prompt-injection`
  counts such text as evidence of injection. Residual risk; not blocking.
