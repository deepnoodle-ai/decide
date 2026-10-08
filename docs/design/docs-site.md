# The documentation site

Status: Implemented in #68, #70 and #72. Written 2026-10-07.
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
our own tokens and a few components. [Theming](#theming) lists the
knobs. We pin exact versions: Starlight 0.42.5 on Astro 7.3.6 today.

`site/go.mod` holds one line, `module github.com/deepnoodle-ai/decide/site`.
A directory with its own `go.mod` is outside the parent module, so `go
build ./...`, `go vet ./...` and `go mod tidy` skip `site/` and its
`node_modules`.

### Pages

```
/                          landing: headline, a playing terminal, three ways in
/start/                    quickstart: install, key, one line, your own git diff
/start/go/                 go get, a key, one program from examples/, go run
/start/answers/            the concepts: question types, probability, flags, providers
/tutorials/
  check-agent-commands/    command-risk before a command runs
  triage-issues/           task-readiness on the demo's issues
  review-pull-requests/    a local diff, then GitHub Actions, then block a merge
/reference/
  cli/  items/  diffs/  templates/  providers/  output/  cache/  claude-code/
/recipes/                  the rest of recipes.md: other CI systems, hooks, export
/404                       a designed not-found page
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

`/start/go/` imports `examples/<name>/main.go` as raw text, so the code
the reader sees is the code that `go build ./...` compiles. In the same
way, a script that a tutorial shows and a tape runs, such as
`checked.sh`, lives once in `site/src/snippets`. `/reference/templates/`
reads each built-in template's `template.json` and `README.md` at build
time, so it can't drift from the templates decide runs. Vite needs
`server.fs.allow` to include the repository root for this. Later
tutorials follow, one pull request each: security bugs with Claude Code,
a Go tutorial, and writing a template.

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
made, set the API key, and point `DECIDE_HOME` at an empty folder, so
no answer comes from the cache. Commands that the reader runs once and that
the tape can't show, such as `brew install` or `export
TYPESAFE_API_KEY=…`, appear only as code blocks on the page.

### Recordings

A recording is a VHS tape. VHS types commands into a real shell and
writes video and still images.

```
# site/tapes/start-diff.tape
Set Height 988                       # 24 rows; settings.tape sets the rest
Hide
Type "sh $REPO/demo/setup.sh shop >/dev/null && cd shop && clear" Enter
Wait
Show
Type "git diff | decide run code-risk" Sleep 500ms Enter
Wait                                 # until the prompt comes back
Sleep 3s                             # the loop holds the last frame
```

A tape holds only its own `Set` lines and commands. `record.sh` puts
`settings.tape` and the theme before them, and a hidden step that starts
in an empty folder with an empty `DECIDE_HOME`, so no answer comes from
the cache.

Each tape writes an MP4, a PNG still of its last frame, and a text
transcript. A tape can clear the screen between scenes, as the landing
tape does for its three commands; the transcript then holds the last frame
of each scene. Their names carry a hash:
`decide/site/<tape>-<hash>.mp4`, `.png` and `.txt` in the R2 bucket
`deepnoodle-public`, which serves `files.deepnoodle.ai`. The hash is the
first 12 hex digits of the SHA-256
of the tape, `settings.tape`, `theme.json`, and the `version` in
`plugin/.claude-plugin/plugin.json`. That version already names the next
release: the pull request that cuts the changelog sets it, and the release
workflow fails if it differs from the tag. The hash gives five
properties:

- A changed tape gets a new URL, so a preview never overwrites the media
  that the live site uses.
- A new release changes every hash. The changelog pull request then fails
  to build until a maintainer runs `record.sh`, so a release can't ship
  with recordings from the release before (R-4).
- No manifest. `Terminal.astro` reads the tape at build time, computes the
  same hash, and builds the URLs.
- "Is this recorded?" is one HTTP request.
- Browsers and the CDN can cache each file forever.

`site/scripts/record.sh [--local] [tape...]` builds decide for Linux from
the working tree and records each tape whose files are missing from R2.
It never replaces an uploaded file, since browsers and the CDN cache it
for a year. It uploads a recording only if the transcript has no `Error:`
line and has each `# Expect:` line of the tape, such as `# Expect: !
severe`, so a caption's claim can't silently go wrong.
It runs VHS in its pinned Docker image (`ghcr.io/charmbracelet/vhs`),
plus `git` for the demo, with the repository mounted, decide on `PATH`,
and `REPO` set, so every recording has the same fonts on any machine. It
adds the theme from `theme.json`. It encodes each MP4 again with tagged
BT.709 color and `-movflags +faststart`, so it plays while it downloads,
and takes the poster from the last frame. `record.sh --local` keeps the
files in `site/tapes/out` instead, and `DECIDE_MEDIA` points a local
build at them. Then it uploads them with
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
  copy button.
- An "Output as text" disclosure, built from the transcript at build
  time. It is the legible version on a phone, the text for screen
  readers, and works with JavaScript off (R-7).

Prose never types a percentage from a recording, since each release
records again. It quotes verdicts and flag rules, such as "flagged on
`destructive`", and takes any number it needs from the transcript.

`landing.tape` also writes `landing.gif`, so the README's GIF is made from
the same tape as the site's hero.

### Checks

- **Commands work: `internal/cli/tapes_test.go`.** It builds decide once
  and starts a `decidetest` server. Its default responder answers every
  question type, and the CLI reaches it through `TYPESAFE_BASE_URL` with
  the key `test-key-00000000`. The test runs each tape's `Type` lines,
  hidden ones too, in `sh` in a fresh temporary directory, with `REPO`
  set. Exit 0 and 2 pass. Exit 1, or `unknown flag` on stderr, fails and
  names the tape. It joins `TestDemo` in `go test ./...`, so a CLI change
  that breaks a tutorial fails in the pull request that makes it (R-5).
- **Tapes parse:** `vhs validate site/tapes/*.tape`, in the same image.
- **Every tape is recorded:** the build fetches each transcript. If one is
  missing, the build fails and prints the `record.sh` command to run.
- **Site builds:** `npm ci`, `astro check`, `astro build`, and
  `starlight-links-validator`, which fails the build on a broken link or
  heading between pages.
- **A tape never runs a command decide should refuse.** The tape test's
  fake server answers `command-risk` as dangerous, so `checked.sh` refuses
  every command there.

### Workflow: `.github/workflows/site.yml`

Triggers: pull requests that touch `site/**`, `demo/**`, `examples/**`,
`plugin/.claude-plugin/plugin.json` or the workflow. Also `v*` tags and
`workflow_dispatch`.

```
site.yml          build ──► deploy   (v* tags, or a dispatch run from a tag)
                    │
site-preview.yml    └─► preview      (pull requests from this repository)
```

1. **build.** The checks above, then uploads `dist/` as an artifact.
2. **preview.** A second workflow, `site-preview.yml`, starts with
   `workflow_run` when a pull request's build ends. GitHub runs it as it
   is on `main`, so a pull request cannot change the code that holds the
   token. It takes only the built files from the pull request. They are
   static, and the Worker runs no code of its own.
   `wrangler versions upload --preview-alias pr-<n>` uploads them without
   going live and returns
   `https://pr-<n>-decide-docs.deepnoodle-inc.workers.dev`. A sticky
   comment on the pull request holds the link. Cloudflare's newer
   Previews (`wrangler preview`) isolate a branch's resources. A static
   site has none, and Previews are in beta, so we use aliased version
   URLs.
3. **deploy.** `wrangler deploy`, in a GitHub environment named `site`
   that only `v*` tags can use. To deploy again, run the workflow from a
   tag: `gh workflow run site.yml --ref v0.3.1`. A run from `main` is
   refused, so the live site never shows unreleased docs.

`site/wrangler.jsonc`:

```jsonc
{
  "name": "decide-docs",
  "compatibility_date": "2026-10-01",
  "assets": { "directory": "./dist", "not_found_handling": "404-page" },
  "routes": [{ "pattern": "decide.deepnoodle.ai", "custom_domain": true }],
  "workers_dev": false,
  "preview_urls": true
}
```

The first deploy creates the DNS record and the certificate. With
`workers_dev` off, the site has one address, and previews still get
theirs.

`CLOUDFLARE_API_TOKEN` can edit Workers on the account and Workers routes
on the `deepnoodle.ai` zone, and nothing else. It is an environment
secret, never a repository secret. A workflow on any branch can read a
repository secret, and a pull request can change its own workflow. Two
environments hold the token:

- `site`, which only `v*` tags can use, for deploys.
- `site-preview`, which only `main` can use, for previews.

No job that runs for a pull request can reach either one. The account ID
is a repository variable, `CLOUDFLARE_ACCOUNT_ID`, since it is not
secret. We use `pull_request`, never `pull_request_target`. A fork's pull
request builds and runs the checks, but gets no preview. A change to
`site-preview.yml` takes effect only after it merges.

### The rest

- **Analytics:** Cloudflare Web Analytics. It uses no cookies, and its
  beacon goes in Starlight's `head` config (R-9). Its site token is the
  repository variable `CF_BEACON_TOKEN`, which `site.yml` passes only to
  builds from a tag, so previews count nothing.
- **Link previews:** `astro-og-canvas` makes a preview image for each page
  at build time, from its title and description, and a route middleware
  adds the `og:image` tag to each page (UC-5). No image is committed, so
  the card has no logo: it shows the title, the description, and
  `decide.deepnoodle.ai` in JetBrains Mono.
- **Performance:** videos load only when they come into view, and each
  loop should be under 1 MB. Pages ship no JavaScript except search, the theme switch
  and the player.

## Theming

Theming has three consumers: the site's CSS, the code blocks, and the
recordings. Each takes its colors and fonts in a different form, so the
risk is that they drift apart. The rule: **one token for each value, and
the fewest overrides that get the look.** Starlight is before 1.0, and
each component override is code we re-check on every upgrade. 0.42
changed the markup of the page frame and the mobile menu, for example.

### Knobs we will set

| Knob | Where | Why |
| --- | --- | --- |
| Accent: `--sl-color-accent-low`, `--sl-color-accent`, `--sl-color-accent-high` | `site/src/styles/theme.css`, in `customCss` | Neutral: near-white in dark, near-black in light. Cyan is kept for answers only, as in the CLI. |
| Grays: `--sl-color-white`, `--sl-color-gray-1` … `-gray-7`, `--sl-color-black` | same | Starlight's grays are cool blue. A warmer or neutral scale is most of what makes a stock site look like ours. |
| Fonts: `--sl-font`, `--sl-font-mono` | same, with `@fontsource/*` packages in `customCss` | Type does more for "elegant" than color does. |
| Heading sizes: `--sl-text-h1` … `-h3`, `--sl-line-height-headings` | same | A larger, tighter title scale for the landing and tutorial pages. |
| Content width: `--sl-content-width` | same | 45rem (720px) on reference pages. 48rem on tutorials, which turn off the table of contents, so recordings show at 720px with room around them. |
| Sidebar | `sidebar` in `astro.config.mjs` | Written by hand in teaching order: Start, Tutorials, Reference, Recipes. Autogenerate would sort them by file name. |
| Code blocks | `expressiveCode` in `astro.config.mjs` | Dark in both themes, like the recordings: one custom dark theme, `useStarlightUiThemeColors: false`. `styleOverrides.frames` gives shell blocks the recording's frame, without the title bar's dots. Blocks over 25 lines collapse. |
| Logo and favicon | `SiteTitle`, `favicon` | A wordmark, `decide` in JetBrains Mono with the version, and an SVG favicon. No mark. |
| `head` | `astro.config.mjs` | The analytics beacon. |
| Terminal palette | `site/tapes/theme.json` | See [Recordings](#theming-the-recordings). |

Light and dark are two full blocks of CSS. Dark is the base, under
`:root`, and light overrides it under `:root[data-theme='light']`.
Starlight's theme editor
(starlight.astro.build/guides/css-and-tailwind/) builds both scales and
checks their contrast against WCAG AA. We start there and save its
output in `theme.css`.

Starlight puts all of its CSS in cascade layers (`@layer starlight.*`),
so our CSS, which has no layer, wins without `!important` or long
selectors.

### Components we write or override

- **`Hero`** (an override). The landing page is a `template: splash` page,
  and its first screen puts the headline beside a playing `Terminal`.
  Starlight's hero can't hold the component: `image.html` is a fixed
  string, and `Terminal` builds its URLs at build time. Without a `hero`,
  Starlight puts the title in its own panel above the body. The override
  reads `hero.title` and `hero.tagline`, and keeps the `<h1>`'s `id` and
  `data-page-title`. It is about 80 lines with no layout code, and we
  re-check it on each upgrade. The rest of the landing page is MDX.
- **`Terminal` and `Screenshot`** are our components. They hold the window
  frame that code blocks also use.
- **`SiteTitle`** (an override) is the wordmark and the release number,
  read from `plugin.json`, so a preview shows the next release.
- **`Footer`** (an override) wraps the default and adds a Deep Noodle
  line.
- **A tutorial's goal and what the reader needs** is an MDX component at
  the top of each tutorial, not a `PageTitle` override.

We do not override `Head`, `Header`, `PageFrame`, `TwoColumnContent`,
`ThemeProvider` or `Sidebar`. Starlight's docs call the layout overrides
complex, and they are the ones that upgrades break. If the design needs
a top navigation bar or a different page layout, that is a separate
decision, with its upgrade cost written down.

### Theming the recordings

- **Always dark.** A dark terminal looks right on a light page, and Charm
  does the same in the VHS README. Two recordings per tape would double
  the files, and a theme switch would have to swap videos. In light mode
  only the frame's shadow and border change.
- **No chrome in the video.** `Padding 0`, with no `WindowBar`, `Margin`
  or `BorderRadius`. The HTML frame draws the title bar, corners and
  padding. It stays sharp at any pixel density, follows the page theme,
  and matches the code blocks. The frame's background reads
  `background` from `theme.json`, so the edges of the video, its still
  image and the frame are the same color.
- **One palette file.** `site/tapes/theme.json` holds VHS's theme fields:
  `background`, `foreground`, `cursor`, `selection`, and the 16 ANSI
  colors. `record.sh` passes it to VHS as `Set Theme {…}`. `Terminal.astro`
  reads it at build time for the frame. Its hash is part of each
  recording's name, so a palette change marks every recording as missing.
- **Recorded at 2x, shown at 1x.** VHS records at one pixel per CSS
  pixel and has no 2x mode, so text blurs on retina screens. A tutorial
  tape is 1440×840 at `FontSize 28` (85×22 cells) and shows at 720px.
  The landing tape is 1160×644 (69×17 cells). The two sizes are two
  settings files.
- **Written for the last frame.** The last frame is the poster and the
  still for reduced motion, and ends the transcript, so it must show what
  the caption claims. The landing tape ends on a flagged command. A step that prints more than 22 rows records less.
- **Smaller files.** `Framerate 30` and `CursorBlink false`. Typing
  doesn't need 50 frames a second, and a blinking cursor changes every
  frame.
- **The mono font must be the same in both places.** VHS draws with
  fonts installed in its image and can't load a web font. The site's mono
  font must be one that is both in the VHS image and on Fontsource:
  JetBrains Mono, IBM Plex Mono, Fira Code, Source Code Pro, Inconsolata
  or Hack. A font outside that list, such as Geist Mono, needs our own
  VHS image. VHS can't draw ligatures, so choose a font that reads well
  without them.

### What we don't use

- **Tailwind.** `@astrojs/starlight-tailwind` works, but it adds a layer
  order to keep right, and a docs site needs a few dozen custom
  properties, not utility classes.
- **A community theme.** Rapide, Black, Nova and others are well kept,
  but each one pins us to its release schedule, and Nova overrides 14
  components, including the page frame. We read them for ideas. Rapide
  shows how far tokens alone go, and Black and the Astro and Biome docs
  show what overrides can do.

### The design

[docs-site-look.md](docs-site-look.md) sets the values inside these
knobs: the palette, Inter and JetBrains Mono, the heading scale, the
landing page, the frames, the tutorial page and motion. The
implementation PR shows screenshots of the landing, a tutorial and a
reference page, in light and dark, at 1280, 1024 and 390 wide, and gets
an independent design review. One real recording is checked on a
phone before the merge.

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
  the next tag. That is the cost of R-2.
- Recordings show what the model said on the day they were made. Between
  releases, a model update can make a recording differ from a new run.
- Each release adds a manual step: `record.sh`, a few minutes and
  one provider call per item per tape.
- Pull requests that change tapes, from forks or not, need a maintainer to
  record them.
- Moving the guides changes where contributors edit the docs. AGENTS.md
  and CONTRIBUTING.md must say so, or agents will keep editing the pointer
  files.

## Rollout

1. The PRD PR holds the PRD and this spec.
2. Three implementation PRs, each building on the one before. Their
   preview links are the review surface.
   1. The site: Starlight, the theme, the landing page with a terminal
      still, `/start/`, `/start/go/`, `/start/answers/`, the 404 page and
      the workflow.
   2. Recordings: the tapes, `record.sh`, the `Terminal` player,
      transcripts and the tape test.
   3. Content: the tutorials, the reference and recipes moved from
      `docs/`, link previews, and the README, AGENTS.md and changelog.
   A design review covers each, with screenshots in light, dark and
   phone widths.
3. Before the first preview, a maintainer adds the variable, the `site`
   and `site-preview` environments with the secret in each, and creates the Worker once with no route:
   `wrangler deploy` with a copy of `wrangler.jsonc` that has no
   `routes`. Wrangler uploads a preview only for a Worker that exists, and
   this one has no public address until the first tag. Before the third PR merges, a maintainer
   runs `record.sh`.
4. The first `v*` tag after the merge deploys the site. Then we add the
   site to the repository's homepage field and to the CHANGELOG.

## Follow-up: agent docs and answer-type recordings

The launch follow-up in #74 adds `/reference/agents/` and `/llms.txt`. The
index links to Markdown copies of the reference pages. The copies are made
from the existing `.md` sources at build time, with their title and description;
there is no second authored guide and no MDX-to-Markdown parser. MDX tutorials
keep their human-readable site links. Each HTML reference page advertises its
Markdown alternate and the llms index.

The three answer-type panels use real runs from small example templates in
`site/src/snippets`, with commands, transcripts, and still images. They reuse
the existing player, so only one video plays and reduced motion never autoplays.
The templates and tapes run through `TestTapes`; they are examples, not new
built-in templates.
