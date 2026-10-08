# Learn decide by watching it work

Status: Shipped in #68, #70 and #72
PRD PR: #67  Implementation PRs: #68, #70, #72  Updated: 2026-10-08

The initial site launched with v0.4.0. Follow-up tutorials and agent docs are
tracked in [the roadmap](../roadmap.md). Newcomer timing is still pending (#78).

## Problem

A developer who hears about decide has one place to learn it: the GitHub
repository. The README is 232 lines, and it links to four guides of 160
to 540 lines each. They are accurate, but they are reference text. They
tell the reader what each flag does. They do not show a run from start
to finish. There is one picture, the GIF at the top of the README.

This costs us twice:

- **Evaluation.** A decision model is a new idea. Most developers know
  only text-generating models. They understand decide the moment they see
  a typed answer with a probability come back for each item. Today they
  must install it, get a key, and run it before they see that.
- **Adoption.** The jobs decide does well, such as a gate in CI, a check
  on an agent's commands, or a security audit in Claude Code, are spread
  across `docs/cli.md`, `docs/recipes.md` and `docs/claude-code.md`. No
  page walks one job from an empty shell to a working result.

A repository is also hard to share. A link to `docs/recipes.md` on
GitHub does not look like a product. A post or a talk about decide needs
a page that does.

If we do nothing, decide stays a tool that people understand only after
they try it, and fewer people try it.

## Context

- **Docs today.** The README, `docs/cli.md` (the CLI reference),
  `docs/recipes.md` (CI and hooks), `docs/claude-code.md` (the plugin),
  `examples/README.md`, and package docs on pkg.go.dev.
- **Media today.** One GIF and one MP4, recorded by hand on 2026-10-02
  and served from `files.deepnoodle.ai`. Nothing in the repository can
  make them again.
- **The demo fixture.** `demo/` holds a small store backend, issues,
  pull requests and shell commands with planted problems. `TestDemo`
  keeps it in step with the CLI. It is the natural set for recordings.
- **Comparable tools.** Developer tools that teach well, such as Charm,
  Astro and Bun, share a pattern: a short landing page with a live
  terminal, a quickstart that finishes in minutes, tutorials built around
  one job each, and reference pages after that. Charm records its
  terminal demos from scripts with VHS, so the demos change with the
  code.
- **Hosting.** `deepnoodle.ai` uses Cloudflare DNS on our account.

## Users and job

- **Primary:** a developer who meets decide for the first time, through a
  post, a talk, a workshop or a link from a teammate. The job: "Show me
  what this does and whether it fits my problem, then get me to a first
  result in a few minutes." We optimize for this reader.
- **Secondary:** a developer who already uses decide and comes back to do
  a new job, such as adding a CI gate, or to look up a flag.

## Use cases

### UC-1: See what decide does

The developer opens `decide.deepnoodle.ai`. The first screen shows one
sentence about decision models, a terminal that plays a real run, and
three ways in: the CLI, Go, and Claude Code.

Acceptance:
- [x] At 1280×720 the first screen shows the sentence, the playing
      terminal, and the three ways in, with no scroll. At 390×844 it
      shows the sentence and the terminal, and the rest follows.
- [x] The terminal shows typed answers with probabilities across real
      example runs.
- [x] A reader who never scrolls can still find the install command.

### UC-2: Get a first result

The developer follows the quickstart. They install decide, set a key for
one provider, ask a question about one line of text, then run a built-in
template on a `git diff` in their own repository. The quickstart needs no
files from us. Each step shows the command, and the runs show a
recording of their output.

Acceptance:
- [x] The quickstart has at most five steps, and each step shows the
      exact command to copy.
- [x] Each command has a copy button.
- [x] The reader's output has the same shape as the recording: the same
      kind of lines, and a typed answer with a probability for each item.
- [x] The last step tells the reader what to try next.

### UC-3: Do one job from start to finish

The developer picks a tutorial for their job. Each tutorial starts from
nothing and ends with the job done. Where it needs sample data, its
first step gets the demo with one command. The site launches with three:

1. Check a shell command before an agent runs it.
2. Triage new issues.
3. Review pull requests: on a local diff, then in GitHub Actions, and
   block a merge.

These follow, one pull request each: find security bugs with Claude
Code, make a decision from Go, and write your own template.

Acceptance:
- [x] Each tutorial names its goal and what the reader needs in its first
      lines.
- [x] Each tutorial shows a real run: a terminal recording, or a
      screenshot where the result is not in a terminal, such as a check
      on a pull request.
- [x] Each command in a tutorial runs as shown, from a fresh shell that
      followed the earlier steps.
- [x] Each tutorial ends with a working result: a hook, a workflow file,
      or a saved run.

### UC-4: Look something up

The developer searches for a flag, an exit code or an output format, and
lands on the reference.

Acceptance:
- [x] Search finds each CLI flag and each built-in template by name.
- [x] The reference covers what `docs/cli.md` covers today.
- [x] Go API questions link to pkg.go.dev. The site does not copy them.

### UC-5: Share a page

The developer pastes a link into Slack, a post or a slide.

Acceptance:
- [x] Each page has a title, a description and a preview image for link
      unfurls.

## Requirements

- R-1: The site lives at `https://decide.deepnoodle.ai`.
- R-2: The live site describes the latest release, not unreleased work
  on `main`.
- R-3: Each pull request that changes the site gets a preview link.
- R-4: Each terminal recording comes from a script in the repository
  and runs the real `decide`. A maintainer makes them all again with one
  command. Screenshots of results outside a terminal are made by hand.
  The recordings match the latest release. A release can't go out
  while a recording is older than it.
- R-5: A change that breaks a recorded command fails CI on the pull
  request that makes it. Go code in a tutorial comes from `examples/`,
  so the build compiles it.
- R-6: Pages load fast on a phone. Recordings load only when they come
  into view, and each one has a still image until then.
- R-7: The site works with JavaScript off, apart from search and
  playback. Each recording has a still image and its commands as text.
- R-8: The site has a light and a dark theme, and follows the reader's
  setting.
- R-9: We can count visits per page without cookies.
- R-10: The README stays the front door on GitHub. It keeps its demo and
  install steps, and links to the site for the rest.

## Not in scope

- **A domain of its own.** `decide.deepnoodle.ai` costs nothing now, and
  a redirect can move it later.
- **Docs for each version.** Before v1 there is one supported version,
  the latest.
- **A Go API reference.** pkg.go.dev does that well.
- **A blog, a changelog page or a pricing page.** The GitHub releases
  hold the changelog. decide has no price.
- **`llms.txt` and pages for agents.** Useful, and a small follow-up.
  It does not change the design.
- **Translations.**

## Decisions

- **On `decide.deepnoodle.ai`.** It costs nothing, it builds the Deep
  Noodle name, and "decide" alone is too common a word to own.
  Rejected: buy `decide.sh` now. We can buy it later and redirect.
- **In this repository.** Docs change in the same pull request as the
  behavior they describe, as AGENTS.md requires, and CI can run the
  tutorials' commands against the CLI. Rejected: a separate repository,
  which splits each change into two pull requests that drift apart.
- **The site holds the user guides.** `docs/cli.md`, `docs/recipes.md`
  and `docs/claude-code.md` move into the site, so there is one copy.
  `docs/` keeps what contributors read: PRDs, designs, the roadmap and
  release steps. Rejected: keep both, which doubles each doc change.
- **Instructive first.** Tutorials come before reference in the
  navigation, and each one does one job. Rejected: a site organized by
  command, which is what `docs/cli.md` is today.
- **Video, not GIF.** Recordings play as short muted loops of video with
  a still image first. A GIF of the same run is several times larger and
  blurrier. The README keeps its GIF, since GitHub can't autoplay video.

## Success

- A developer who has never seen decide and has an API key can go from
  the landing page to a first result in under five minutes. We time
  getting a key on its own, since the provider's sign-up is outside our
  control. We check this by watching two or three people from a workshop
  do it.
- Visits per week, and the share of visitors who open the quickstart.
- Must not get worse: Go CI time, and the effort to make a docs change.
  A docs change still takes one pull request.

## Risks and open questions

- **Recordings need a live provider.** A real run needs an API key and
  can answer a little differently each time. A maintainer records with
  one command before each release, and CI checks that every recording
  exists for the version being released. Not blocking.
- **Moving the guides breaks links.** The plugin, the README, the
  CHANGELOG and posts link to `docs/cli.md` and to sections in it. Each
  moved file becomes a short table of its old headings and their new
  URLs. Not blocking.
- **The look.** "Beautiful and elegant" is the point, and a stock theme
  is not enough. The landing page and the terminal frame get a design
  pass and an independent design review before launch. Not blocking.
