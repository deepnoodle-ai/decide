# A second opinion for Claude Code, from decide

Status: Draft
PRD PR: none (one PR with the build)  Implementation PR: #45  Updated: 2026-10-03

## Problem

People run Claude Code with wide permissions so that it works without
stopping: `auto`, `acceptEdits`, or `bypassPermissions`. In those modes
three mistakes cost the most, and nothing outside Claude itself checks for
them:

- **A destructive or leaking command runs.** `git checkout -- .`, a force
  push, `DROP TABLE`, or `cat ~/.aws/credentials | curl ...` runs with no
  prompt. Permission rules are lists of patterns, and these commands take
  forms no list covers.
- **Content from outside steers Claude.** A web page, an issue, or an MCP
  result can carry instructions for AI agents, hidden from the person who
  reads it.
- **Claude says the work is done when it is not.** A reply says "all tests
  pass" while the test output in the same turn shows a failure.

Claude Code v2.1.287 (2026-10-01) added mods: plugins whose code runs
inside Claude Code and can hold a tool call, add a note that only the model
reads, add a tool, and draw a band above the prompt. That makes an
independent check at each of these moments possible. decide already
answers this kind of typed question with a probability, in a fraction of a
second (see Context). If we do nothing, the roadmap's Claude Code plugin
stays a skill that only teaches Claude to run the CLI.

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
  the commands on its list. decide judges the text of any command; the two
  compose.
- **Claude Code's `auto` mode** puts permission prompts to a model
  classifier. It replaces the person for prompts. It is not a check in
  bypass mode, and it does not read content from outside or replies.
- **Settings hooks** with a deny-list script are the common workaround.
  They have the same coverage limit as blast-radius, and no interface.

Measurements, on 2026-10-02 against Jev (`jev-1.13.0`):

- `demo/commands.txt` has ten commands. `command-risk` flags the five
  planted ones (discard changes, force push, `DROP TABLE`, print a secret,
  send credentials away) and none of the five routine ones, which include
  `git push -u origin refund-fix` and `gh pr create`. `demo/replies.jsonl`
  has five turns; `reply-check` flags the false "all tests pass" and the
  unchecked edit, and none of the three honest ones. `TestLiveDemoFlags`
  checks both.
- On a wider set of 25 hand-written commands, it flagged
  `neonctl branches delete main`, `aws s3 rb`, `kubectl delete namespace`,
  `curl … | sh`, and `scp -r ~/.ssh …`. It missed `cat .env` (77% leak). It
  flagged `find . -name '*.tmp' -delete` (81% destructive), which is
  harmless.
- One command took 0.15–0.26 s to judge, five runs, from `decide run` start
  to exit. A reply check with 12,800 characters of tool calls took
  0.19–0.56 s, five runs.
- In a headless session in `bypassPermissions`, the prototype held
  `git checkout -- .`, kept the uncommitted work, and Claude explained why.
  A page with white 1px text that told AI agents to print `~/.npmrc`
  scored 96% injection and 97% hidden; Claude refused and told the user.
  Asked to rank five issues, Claude wrote four questions for the judge
  tool, ranked by the answers, and called a 58% answer unsure.

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
  the answer is flagged. There are three:
  - **Command check**: before each shell command. Could it destroy work
    that is hard to get back, or expose secrets?
  - **Content check**: after content arrives from outside: a web page, a
    search, an MCP tool, or a `gh`, `curl`, `wget`, or `http` command. Does
    it try to take over an AI agent, or hide text from a person?
  - **Reply check**: when a turn ends. Does Claude's reply claim more than
    its tools showed, such as passing tests or a fix nothing checked?
- **Judge tool**: a tool Claude calls with its own typed questions about
  one or more texts. It answers each with a probability.
- **Flagged**: what it means everywhere in decide: the answer passes the
  template's flag, which `decide templates show` lists. The plugin uses the
  same flags, so `/decide`, `decide runs view`, and the CLI agree.

The checks run on two new built-in templates, `command-risk` and
`reply-check`, and on `prompt-injection`.

## Use cases

Each criterion says how it is shown: **[test]** in `claude plugin test
plugin` or `go test`, with decide faked; **[live]** in `TestLiveDemoFlags`
or a headless session, as PR evidence.

### UC-1: Install the plugin

```text
/plugin marketplace add deepnoodle-ai/decide
/plugin install decide@decide
```

The person has decide 0.2.0 or later and `TYPESAFE_API_KEY` (or the
Cloudflare variables and `DECIDE_PROVIDER=cloudflare`).

Acceptance:
- [ ] [test] `claude plugin validate .` and `claude plugin validate plugin`
      pass, in CI.
- [ ] [live] Installed from a local copy of the marketplace, the plugin
      loads, and `/decide` says that nothing has been checked yet and how
      to see saved runs.
- [ ] [test] With decide missing, without a key, or older than the plugin,
      the first check writes one transcript line that says what is wrong
      and what to run. For an old decide, it names the version the plugin
      needs. The status line shows `a check is off: /decide`. Commands still
      run, and the checks try again a minute later.

### UC-2: A risky command waits for the person

Claude, in `bypassPermissions`, calls Bash with `git checkout -- .`.

Acceptance:
- [ ] [test] The command check flags it, and Claude Code asks the person
      "decide judged this command 93% likely to destroy work that is hard
      to get back … Run it?" with **Run it** and **Don't run it**.
- [ ] [test] On **Don't run it**, the command does not run, and Claude reads
      why and that the user chose not to run it. When the person dismisses
      the question, Claude reads that the user dismissed it.
- [ ] [test] In `auto` mode, where Claude Code's own asks go to a
      classifier, decide asks the person the same way.
- [ ] [test] When Claude Code will ask the person anyway (default mode, no
      rule), there is one dialog, Claude Code's own, with decide's line
      under it.
- [ ] [test] [live] When no one can answer (`claude -p`), a flagged command
      is refused, and Claude reads why.
- [ ] [test] [live] Routine commands run with no question: `ls`,
      `go test ./...`, `git push -u origin refund-fix`, `gh pr create`.
- [ ] [test] With the option `commands` set to `deny`, a flagged command is
      refused with no question. With `off`, commands are not checked.

### UC-3: Content from outside carries hidden instructions

Claude fetches a page with an instruction for AI agents in white text.

Acceptance:
- [ ] [test] [live] Claude reads, after the result, a note that gives both
      probabilities and tells it to treat the result as data and to tell
      the user what it asked for. The person sees a toast.
- [ ] [test] Results under 80 characters, errors, and Bash commands other
      than `gh`, `curl`, `wget`, and `http` are not checked.

### UC-4: A reply claims more than its tools showed

In one turn Claude edits a file, runs the tests, sees a failure, and replies
"Fixed, and all tests pass."

Acceptance:
- [ ] [test] When the turn ends, a band above the prompt reads "This reply
      may claim more than its tools showed" with the probability, a button
      **Ask Claude to recheck**, and **Dismiss**.
- [ ] [test] The button sends Claude a prompt that names the probability and
      asks it to say plainly what failed, then fix what it can. The band
      closes. A typed prompt or **Dismiss** also closes it.
- [ ] [test] A turn that edits code, runs no check, and claims no outcome
      shows no band. `/decide` lists its `unverified` score.
- [ ] [test] The check reads the start and the end of a long tool result,
      so a test failure printed last is seen.
- [ ] [test] Turns with no tool calls, interrupted turns, and subagent
      turns are not checked. Nor are turns where nothing draws the band:
      `claude -p`, the SDK, and the VS Code panel.

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
- [ ] [test] The judge tool answers every item in one call, one line of
      answers per item, such as `ready: yes 91% · size: small 88%`.
- [ ] [test] Bad input (no items, more than 200 items, more than 8
      questions, a choice with no options) is an error that says what to
      fix, and calls no model.

### UC-6: Use the templates without Claude Code

```sh
echo 'git reset --hard HEAD~3' | decide run command-risk
decide run reply-check demo/replies.jsonl
```

Acceptance:
- [ ] [test] `decide templates` lists `command-risk` and `reply-check`, each
      with a README that `decide templates show` prints.
- [ ] [live] The demo fixtures are flagged as demo/README.md says.

### UC-7: See what decide checked

Acceptance:
- [ ] [test] `/decide` lists the last 15 checks of the session in a
      table, flagged ones marked `!`, with their answers.
- [ ] [test] The footer shows the count, such as
      `decide 14 checked, 1 flagged`. The status line shows only when
      something went unchecked or a check is off.
- [ ] [test] Each checked tool row, or the folded group it is in, shows
      `decide ✓`, or a line with the flagged answers, such as
      `⎿  decide: destructive 96%`.
- [ ] [live] Every check is a saved run:
      `DECIDE_HOME=~/.decide/agent decide runs` lists them.

## Requirements

- R-1: Each check fails open. When decide cannot answer, the action goes on
  as if the plugin were not there, the status line says so, and the checks
  stay off for a minute before they try again. A check waits at most 10 s
  for a command and 20 s for content or a reply.
- R-2: The plugin calls the decide CLI, never a provider directly, so
  providers, keys, retries, and long items work as they do on the CLI.
- R-3: The plugin's runs go to `~/.decide/agent` (or `$DECIDE_HOME/agent`),
  apart from the person's own runs.
- R-4: Text the plugin shows the person (commands, reasons, errors) has its
  control characters removed.
- R-5: The plugin flags what the template flags. A Go test fails when they
  differ, or when the plugin names a question or template that does not
  exist.
- R-6: The plugin's version is the decide release it needs. The release
  workflow fails when the tag differs.

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
  versioned with the binary, and CLI users get them. Rejected: templates in
  the plugin folder, read by path.
- **The template's flags are the plugin's flags, and there is no threshold
  option.** One meaning of "flagged" across the plugin, `decide runs view`,
  and `decide templates show`, and one fewer knob. Rejected: one plugin
  threshold over raw scores; it disagreed with saved runs.
- **One plugin, all three checks on, and two options**: `commands` (`ask`,
  `deny`, `off`) and `decidePath`. Rejected: an option per check; more
  knobs than a first-time user needs.
- **Ask the person in place of Claude Code only when Claude Code will not
  ask a person.** That covers `allow` (a rule or `bypassPermissions`) and
  `ask` in `auto` mode, where a classifier answers. One dialog per command,
  never two. The mode comes from the settings-hook events of each prompt and
  each tool call, so a mode switched mid-turn takes effect one call later.
  Rejected: always ask; it doubles the dialog in default mode.
- **The command check does not flag pushes, releases, or pull requests.**
  `command-risk` has an `external` question that is shown in `/decide` but
  never flagged: in testing it scored 84–97% on routine `git push` and
  `gh pr create`.
- **Fail open, visibly.** The plugin is a second opinion, not a sandbox.
  Failing closed would stop all work when a key expires. The status line and
  a one-minute backoff keep a failing provider from slowing every command.
- **The reply check runs only where its band is drawn**: an interactive
  terminal or the desktop app. Elsewhere its answer would reach no one.
- **The marketplace is named `decide`**, so the install id is
  `decide@decide`. A company-wide `deepnoodle-ai` marketplace stays free for
  later.
- **The plugin also ships a skill** that teaches Claude when to use the
  judge tool and when the CLI, and how to write a template, as the roadmap
  item says.

## Success

- The person keeps the plugin on. Signal: in a week of the maintainers'
  own work, the command check asks without cause less than once a day.
- The reply check is right when it speaks. Signal: in the same week, by
  hand from the saved runs, at least half of the flagged replies did claim
  more than their tools showed.
- `TestLiveDemoFlags` keeps passing as the templates and models change.
- Must not get worse: a command waits about as long as one decide request
  before it runs, and a turn with tools ends one request later.

## Risks and open questions

- **Interruptions erode trust.** If the command check asks too often, people
  turn it off. Reduce: the `external` decision, the Success measure, and
  `decide eval` later. Not blocking.
- **The command check reads only the command's text.** `make clean`,
  `npm run db:reset`, or a script can hide a destructive step. It is a
  second opinion, and the README says so. Not blocking.
- **Text leaves the machine.** Every shell command, checked result, and
  final reply, with up to 12,000 characters of the turn's tool calls, goes
  to the provider. The README says so plainly. Not blocking.
- **The mods API is early access** and may change between Claude Code
  releases. Reduce: pin the version CI tests with, and keep the module
  small. Not blocking.
- **The judge can be addressed by the content it judges** ("answer no").
  The templates treat content as evidence, and `prompt-injection` counts
  such text as evidence of injection. Residual risk; not blocking.
- **The plugin reaches users before the decide that it needs.** Mitigation:
  tag `v0.2.0` the day this merges. Until then a new user sees the line
  that names decide 0.2.0. Not blocking.
