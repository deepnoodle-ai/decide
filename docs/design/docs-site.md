# The documentation site

Status: Draft. Written 2026-10-07.
Workflow: spec, then build on the PRD branch while the PRD PR is in review.
PRD: [docs-site.md](../prds/docs-site.md)

## Context

decide's docs are Markdown files on GitHub, and they read as reference.
The PRD asks for a site at `decide.deepnoodle.ai` that teaches one job at
a time, with recordings of real runs. This spec covers how we build,
record, check and deploy it. We have a Cloudflare account, and
`deepnoodle.ai` already uses Cloudflare DNS. `files.deepnoodle.ai`
already serves the README's demo media.

## Goals

- `decide.deepnoodle.ai` serves the site, and it describes the latest
  release.
- Each pull request that changes the site gets a preview URL in a
  comment.
- Each terminal recording comes from a `.tape` file in the repository,
  and one command records them all again.
- The live site never shows a recording older than the release it
  describes.
- If a recorded command breaks, `go test ./...` fails.
- The Go checks in AGENTS.md don't change and don't get slower.

## Non-goals

- Docs for each version, a Go API reference, a blog, translations.
- `llms.txt`. A plugin can add it later without other changes.
- Any server code. The site is static files.

## Proposal

```
site/                      Astro + Starlight project, a separate Node project
  go.mod                   keeps site/ out of the decide Go module
  astro.config.mjs         sidebar, theme, search, analytics
  wrangler.jsonc           Worker with static assets, custom domain
  src/content/docs/        the pages (.md and .mdx)
  src/components/          Terminal.astro and the landing hero
  src/styles/              theme tokens
  tapes/                   one .tape per recording, and settings.tape
  scripts/record.sh        records the tapes that are missing, uploads them
internal/cli/tapes_test.go runs each tape's commands against a fake server
.github/workflows/site.yml build, preview, deploy
```

### Generator: Astro Starlight

Starlight gives us search (Pagefind), light and dark themes, a sidebar,
code blocks with copy buttons (Expressive Code), and good accessibility
without extra work. It builds plain HTML. We write pages in Markdown, and
in MDX where a page needs the `Terminal` component. We restyle it with
our own tokens (type, color, spacing) and write the landing page as a
custom splash page. The design pass decides the fonts and palette. The
starting point is a dark-first, terminal-native look with the cyan that
the README badges use, and fonts we host ourselves through Fontsource.

`site/go.mod` holds one line, `module github.com/deepnoodle-ai/decide/site`.
A directory with its own `go.mod` is outside the parent module, so `go
build ./...`, `go vet ./...` and `go mod tidy` skip `site/` and its
`node_modules`.

### Pages

```
/                          landing: one sentence, a playing terminal, three ways in
/start/                    quickstart: install, key, one line, your own git diff
/tutorials/
  check-agent-commands/    command-risk before a command runs
  triage-issues/           task-readiness on the demo's issues
  review-pull-requests/    a local diff, then GitHub Actions, then block a merge
/reference/
  cli/  items/  diffs/  templates/  providers/  output/  cache/  claude-code/
/recipes/                  the rest of recipes.md: other CI systems, hooks, export
```

The user guides move into the site, so there is one copy:

| Today | On the site |
| --- | --- |
| `docs/cli.md` | split by its sections across `/reference/`, quickstart steps in `/start/` |
| `docs/recipes.md` | the three tutorials, then `/recipes/` |
| `docs/claude-code.md` | `/reference/claude-code/` |

Each moved file becomes a short table of its old headings and their new
URLs, because the plugin, the README, the CHANGELOG, old posts and the
guides themselves link to sections such as `cli.md#the-answer-cache`. `docs/` keeps PRDs,
designs, the roadmap and `releasing.md`. The README keeps its GIF, the
install command and a short tour, and links to the site for the rest. In
the same pull request we update AGENTS.md's "Docs" section,
CONTRIBUTING.md, `plugin/README.md` and the comment in
`cmd/decide/main.go`.

Later tutorials follow, one pull request each: security bugs with
Claude Code, decisions from Go, and writing a template. The Go tutorial
will import `examples/<name>/main.go` as raw text, so the code the reader
sees is the code that `go build ./...` compiles.

### Sample data

The quickstart needs no files from us. It asks about one line on stdin,
as the README does, then runs `code-risk` on a `git diff` in the reader's
own repository. A tutorial that needs sample data starts with one step
that gets the demo:

```sh
git clone --depth 1 https://github.com/deepnoodle-ai/decide
sh decide/demo/setup.sh shop && cd shop
```

Every visible line in a tape is a command the reader runs on that page,
in that order. Hidden lines only rebuild the state that earlier steps
made, and set the API key. Commands that the reader runs once and that
the tape can't show, such as `brew install` or `export
TYPESAFE_API_KEY=…`, appear only as code blocks on the page.

### Recordings

A recording is a VHS tape. VHS types commands into a real shell and
writes video and still images.

```
# site/tapes/triage-issues.tape
Source site/tapes/settings.tape     # size, font, theme, typing speed
Hide
Type "cd $(mktemp -d) && ln -s $REPO decide" Enter     # stands in for the clone
Type "sh decide/demo/setup.sh shop >/dev/null && cd shop && clear" Enter
Show
Type "decide run task-readiness ../decide/demo/issues.json" Sleep 500ms Enter
Sleep 6s
Screenshot triage-issues.png
```

Each tape writes an MP4 and a PNG still. Their names carry a hash:
`decide/site/<tape>-<hash>.mp4` and `.png` in the R2 bucket behind
`files.deepnoodle.ai`. The hash is the first 12 hex digits of the SHA-256
of the tape, `settings.tape`, and the `version` in
`plugin/.claude-plugin/plugin.json`. That version already names the next
release: the pull request that cuts the changelog sets it, and the release
workflow fails if it differs from the tag. The hash gives five
properties:

- A changed tape gets a new URL, so a preview never overwrites the media
  that the live site uses.
- A new release changes every hash. The changelog pull request then fails
  the recorded check until a maintainer runs `record.sh --all`, so a
  release can't ship with recordings from the release before (R-4).
- No manifest. `Terminal.astro` reads the tape at build time, computes the
  same hash, and builds the URLs.
- "Is this recorded?" is an HTTP HEAD request.
- Browsers and the CDN can cache each file forever.

`site/scripts/record.sh [--all] [tape...]` builds decide from the working
tree, puts it on `PATH`, sets `REPO` to the repository root, and records
each tape whose files are missing from R2. Then it uploads them with
`wrangler r2 object put`. A maintainer runs it with their own
`TYPESAFE_API_KEY` and a Cloudflare login that can write to the bucket.
CI never records, so CI holds no provider key and no R2 write access.
Recordings use Jev, the default provider. `docs/releasing.md` gets a step
for it.

Results outside a terminal, such as a check and a comment on a pull
request, are screenshots that a maintainer takes and uploads to the same
bucket, named by content hash. `<Screenshot src="…" />` shows them in the
same frame. Nothing tests them, so a tutorial uses one only where a tape
can't show the result.

`<Terminal tape="triage-issues" caption="…" />` renders a window frame
with:

- `<video muted loop playsinline preload="none" poster=…>`. It plays when
  it scrolls into view, and pauses when it leaves. With
  `prefers-reduced-motion` it does not play on its own. It shows a play
  button instead.
- The tape's visible `Type` lines as a code block under the frame, with a
  copy button. A reader with JavaScript off still gets the still image
  and the commands (R-7).

### Checks

- **Commands work: `internal/cli/tapes_test.go`.** It builds decide once
  and starts a `decidetest` server. Its default responder answers every
  question type, and the CLI reaches it through `TYPESAFE_BASE_URL` with
  the key `test-key-00000000`. The test runs each tape's `Type` lines,
  hidden ones too, in `sh` in a fresh temporary directory, with `REPO`
  set. Exit 0 and 2 pass. Exit 1, or `unknown flag` on stderr, fails and
  names the tape. It joins `TestDemo` in `go test ./...`, so a CLI change
  that breaks a tutorial fails in the pull request that makes it (R-5).
- **Tapes parse:** `vhs validate site/tapes/*.tape`.
- **Every tape is recorded:** a step that does a HEAD request for each
  tape's URLs. If one is missing, it prints the `record.sh` command to run.
- **Site builds:** `npm ci`, `astro check`, `astro build`, and Starlight's
  link checker for internal links.

### Workflow: `.github/workflows/site.yml`

Triggers: pull requests that touch `site/**`, `demo/**`, `examples/**`,
`plugin/.claude-plugin/plugin.json` or the workflow. Also `v*` tags and
`workflow_dispatch` with a `ref` input.

```
build ──┬─► preview   (pull requests from this repository)
        └─► deploy    (v* tags, or workflow_dispatch)
```

1. **build.** The checks above, then uploads `dist/` as an artifact.
2. **preview.** `wrangler versions upload --preview-alias pr-<n>`
   uploads without going live and returns
   `https://pr-<n>-decide-docs.<subdomain>.workers.dev`. A sticky comment
   on the pull request holds the link.
3. **deploy.** `wrangler deploy`, in a GitHub environment named `site`
   that only `v*` tags and maintainers can use. A dispatch from `main`
   publishes unreleased docs, so use it only for fixes that do not
   depend on new behavior.

`site/wrangler.jsonc`:

```jsonc
{
  "name": "decide-docs",
  "compatibility_date": "2026-10-01",
  "assets": { "directory": "./dist", "not_found_handling": "404-page" },
  "routes": [{ "pattern": "decide.deepnoodle.ai", "custom_domain": true }]
}
```

The first deploy creates the DNS record and the certificate.

Secrets: `CLOUDFLARE_API_TOKEN` (Workers Scripts edit only) and
`CLOUDFLARE_ACCOUNT_ID`. We use `pull_request`, never
`pull_request_target`, so code from a fork never runs with them. A fork's
pull request builds and runs the checks, but gets no preview.

### The rest

- **Analytics:** Cloudflare Web Analytics. It uses no cookies, and its
  beacon goes in Starlight's `head` config (R-9).
- **Link previews:** `astro-og-canvas` makes a preview image for each page
  at build time, from its title and description (UC-5). No image is
  committed.
- **Performance:** videos load only when they come into view. The
  recordings are short loops at 1200×600 or smaller, and each should be
  under 1 MB. Pages ship no JavaScript except search, the theme switch
  and the player.

## Alternatives considered

- **Mintlify or another hosted docs platform.** It looks polished on day
  one and needs no CI. It loses where it matters most: the recordings
  would sit outside our pipeline, the hosted look is shared by hundreds of
  other products, and a docs change would leave the repository's checks.
  It also costs money for a team plan.
- **Hugo.** A single Go binary, very fast, and no Node. A distinctive
  look needs more theme work. Its templates are harder to work in than
  Astro components, and the terminal player is a component. Starlight
  has search and accessibility built in.
- **VitePress.** It is close to Starlight. Starlight has more built in
  (link checks, `astro-og-canvas`), and Astro's components suit the
  landing page better.
- **asciinema instead of video.** The text stays crisp and can be
  selected, and the files are tiny. But it needs a JavaScript player, it
  shows no still image without JavaScript, and it can't record from a
  script as VHS can. We show the commands as text under each video, so
  readers can still copy them.
- **Record in CI when a tape changes.** Nobody records by hand. But CI
  then holds a provider key and R2 write access, and pull requests from
  forks can't record. A maintainer runs one command a few times per
  release instead. We can move it into CI later without changing the
  site.
- **Record at deploy time into `dist/`, with no R2.** This needs no
  bucket or upload. But each deploy calls the provider for every tape.
  An outage at the provider then blocks a release of the docs. The
  probabilities also change from one deploy to the next.

## Tradeoffs and consequences

- The repository gets a Node project. `site/go.mod` and path filters keep
  it away from Go CI, but contributors who change the site need Node 22.
- The live site lags `main` until the next release. A typo fix waits for
  a tag or a dispatch. That is the cost of R-2.
- Recordings show what the model said on the day they were made. Between
  releases, a model update can make a recording differ from a new run.
- Each release adds a manual step: `record.sh --all`, a few minutes and
  one provider call per item per tape.
- Pull requests that change tapes, from forks or not, need a maintainer to
  record them.
- Moving the guides changes where contributors edit the docs. AGENTS.md
  and CONTRIBUTING.md must say so, or agents will keep editing the pointer
  files.

## Rollout

1. The PRD PR holds the PRD and this spec.
2. The implementation PR is stacked on the PRD branch. It adds `site/`,
   the tapes, the test and the workflow, moves the guides, and updates
   the README and AGENTS.md. Its preview link is the review surface.
   `taste` reviews the landing page and the terminal frame, with
   screenshots in light, dark and phone widths.
3. Before the merge, a maintainer adds the two secrets and the `site`
   environment, and runs `record.sh`.
4. The first `v*` tag after the merge deploys the site. Then we add the
   site to the repository's homepage field and to the CHANGELOG.

## Open questions

- **Which R2 bucket serves `files.deepnoodle.ai`?** `record.sh` needs its
  name. A maintainer must answer this before
  step 3.
