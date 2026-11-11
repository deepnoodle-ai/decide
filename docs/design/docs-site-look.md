# The documentation site: design

Status: Implemented in #68, #70 and #72. Written 2026-10-07.
Spec: [docs-site.md](docs-site.md), whose "Theming" section sets the
knobs this doc fills in. PRD: [docs-site.md](../prds/docs-site.md).

This design was drafted from real `decide` v0.3.0 output against a fake
server. The shipped site uses recordings of live provider runs.

## Research: what we take from whom

| Site | The move we take |
| --- | --- |
| [bun.sh](https://bun.sh) | The second column of the hero is data, with a "replay" link. Our bars are probabilities. |
| [ghostty.org](https://ghostty.org) | The terminal is the largest thing on the first screen, and the copy around it is short. |
| [opencode.ai](https://opencode.ai), [atuin.sh](https://atuin.sh) | The install command sits on the first screen, ready to copy. |
| [VHS](https://github.com/charmbracelet/vhs), [charm.sh](https://charm.sh) | Recordings come from scripts and are always dark. VHS's `.txt` output makes golden transcripts. |
| [Stripe docs](https://docs.stripe.com/payments/quickstart), [Tailwind docs](https://tailwindcss.com/docs) | Code panels stay dark on a light page. |
| [fly.io/docs](https://fly.io/docs/) | Teach first: "Step 1: install, Step 2: run". |
| [Tigris docs](https://www.tigrisdata.com/docs/) | A shell block carries a "terminal" bar. Code blocks and terminals share one frame. |
| [linear.app/docs](https://linear.app/docs) | Neutral grays, no illustrations. |
| [SST](https://ion.sst.dev/docs/), [vlt](https://docs.vlt.sh), [Biome](https://biomejs.dev), [Astro docs](https://docs.astro.build/en/getting-started/) | Starlight sites that look distinct with tokens alone: mono group labels and a teaching order in the sidebar. |
| [deepnoodle.ai](https://deepnoodle.ai) | The parent brand: `#0B0C0E`, neutral grays, Inter, JetBrains Mono, easing `cubic-bezier(0.2,0,0,1)`. |

## Principles (each one can be tested)

1. **Every bar is a probability.** No meter or track appears unless it shows an
   answer's probability or score. The player's progress line is the one
   exception.
2. **Cyan and the verdict colors mean answers, and nothing else.** Red means
   flagged, green means passed, yellow means unsure, and cyan means an
   unflagged value. These are the CLI's own rules (`render.go`, `verdict.style`).
   Links, buttons, focus rings and the sidebar are neutral. Test: grep for
   these tokens outside answer components and the terminal. Three landing page
   exceptions are not answer colors at all: the window dots on its terminals
   (macOS's own), the providers' marks (each company's own), and the ✓ and ✗
   beside the licence terms, which are verdicts on what the licence allows.
3. **What is shown is what decide prints.** Every terminal and transcript comes
   from a real run. Prose quotes verdicts and flag rules ("flagged on `leak` and
   `severe`"), never a percentage.
4. **First screen, first command.** At 1280×720 the first screen holds the
   release line, the headline, the tagline and the install command; the
   recordings start just below it. At 390×844 it holds the headline, the
   command and where the key goes.
5. **Only the terminal is dark in light mode.** Recordings and code blocks are
   dark. Everything else follows the theme.

## The core idea: the answer line

The thing that makes decide click is a typed answer with its probability. The
site gives that its own unit, **the answer line**: `[gutter] key  answer  NN%`,
in mono, colored by verdict, over a meter. The meter is a heavy fill on a 1px
track, echoing the CLI's `━` on `─`. It appears in these places:

- **Section 2**, one panel for each type. Each panel leads with plain words
  ("Yes or no"), with `noul` in small mono after them.
- The tutorial cards, the goal block, **OG images** (the page title above that
  page's answer line, such as `! severe yes`), and the **404** page
  (`! page  exists  no`). Neither of the last two shows a percentage.

## Palette

The accent is neutral, so cyan means only "answer value", as in
the CLI.

| Token | Dark (base, `:root`) | Light (`[data-theme='light']`) |
| --- | --- | --- |
| `--sl-color-accent-low` / `accent` / `accent-high` | `#1A1B1F` / `#F3F4F5` / `#F3F4F5` | `#E9EAEC` / `#0B0C0E` / `#0B0C0E` |
| `--sl-color-white` | `#F3F4F5` | `#0B0C0E` |
| `--sl-color-gray-1` | `#DADCE0` | `#1C1D21` |
| `--sl-color-gray-2` (body) | `#B4B7BD` | `#33363D` |
| `--sl-color-gray-3` (muted) | `#7E828A` | `#5C6068` |
| `--sl-color-gray-4` (meter track) | `#5C6068` | `#8A8D94` |
| `--sl-color-gray-5` (hairline) | `#2E3036` | `#D4D6DA` |
| `--sl-color-gray-6` | `#1A1B1F` | `#E9EAEC` |
| `--sl-color-gray-7` | `#131417` | `#F5F5F6` |
| `--sl-color-black` (page) | `#0B0C0E` | `#FFFFFF` |
| `--ans-flag` / `pass` / `unsure` / `value` | `#F7666F` / `#5FD38D` / `#F0C04F` / `#4CCBE2` | `#C2343F` / `#16804A` / `#9A6300` / `#0A7A90` |

Also set these tokens:

- `--sl-color-hairline: var(--sl-color-gray-5)`. Starlight's gray-6 is too faint on `#0B0C0E`.
- `--sl-color-bg-nav` and `--sl-color-bg-sidebar` set to `--sl-color-black`.

And these styles:

- **Links:** `--sl-color-white`, with a 1px gray-4 underline that turns solid on hover.
- **Primary button:** a pill filled with `--sl-color-white`, with black text.
- **Current sidebar item:** a gray-6 fill, white text in weight 600, and a 2px inset bar on the left. Set it in CSS: it is not a component override.
- **Focus:** a 2px solid gray-1 outline with a 2px offset.

**Contrast (WCAG 2.x):**

| Pair | Dark | Light |
| --- | --- | --- |
| Body text on page | 9.7 | 12.1 |
| Muted text on page | 5.1 | 6.3 |
| Links | 17.8 | 19.6 |
| Button text on its fill | 17.8 | 19.6 |
| Current sidebar item | 15.6 | 16.3 |
| Flag / pass / unsure / value on page | 6.6 / 10.4 / 11.5 / 9.7 | 5.4 / 5.0 / 5.1 / 5.0 |
| Meter track, a graphic (1.4.11 needs 3:1) | 3.1 | 3.3 |
| Focus ring | 14.3 | 16.8 |

Links differ from body text by their underline as well as their color, which
WCAG 1.4.1 requires.

**`site/tapes/theme.json`.** `brightBlack`, `#8A8E96`, is used for the `$`
prompt and for syntax comments, and gives 5.7:1 on the terminal background
and 5.4:1 on the commands strip.

```json
{ "name": "decide", "background": "#121316", "foreground": "#ECEDEF",
  "cursor": "#ECEDEF", "selection": "#24424A",
  "black": "#1A1B1F", "red": "#F7666F", "green": "#5FD38D", "yellow": "#F0C04F",
  "blue": "#6EA8FE", "magenta": "#C69CF4", "cyan": "#4CCBE2", "white": "#C9CCD1",
  "brightBlack": "#8A8E96", "brightRed": "#FF8A91", "brightGreen": "#86E3AA",
  "brightYellow": "#F7D47F", "brightBlue": "#96C0FF", "brightMagenta": "#DABBFA",
  "brightCyan": "#8FE3F1", "brightWhite": "#F7F8F9" }
```

The foreground is 15.9:1 and dim text (the foreground at 50%) is 4.7:1. Bold
text renders in `brightWhite`.

## Typography

- **Sans:** Inter (`@fontsource-variable/inter` with `opsz`, and
  `font-optical-sizing: auto`).
- **Mono:** JetBrains Mono 400 and 700 (in the VHS image), with
  `font-variant-ligatures: none`.
- **Text:** body 16px with line height 1.65, and paragraphs capped at `68ch`.
  Mono is 14px in recordings and 13.5px in code blocks.
- **Headings:** `--sl-text-h1: clamp(2rem, 1.4rem + 1.6vw, 2.5rem)` with
  −0.03em tracking. `--sl-text-h2: 1.5rem`, `--sl-text-h3: 1.25rem`,
  `--sl-line-height-headings: 1.15`, weight 600.
- **Landing headline:** 50px at ≥1100px, 44px at 800–1099px, and 38px on
  phones. Line height 1.04, tracking −0.035em.
- **Wordmark (SiteTitle):** the character's face at 32px in the title's color, then
  `decide` in JetBrains Mono 700 at 19px, then the version in mono 12px
  gray-3. The version is read from `plugin.json`, so PR
  previews show the next version. The asking face looks up at three
  thought dots; hover the wordmark or the footer line and the dots light up
  in turn. Reduced motion keeps it still.
- **Favicon:** the `mark` face, light on dark or dark on light to follow
  the system theme. Not cyan, following principle 2.

## Landing page

**Header: Starlight's own.**

- **≥50rem:** the title, a centered search box (at most 22rem), the GitHub
  icon, a divider, and the Dark/Light/Auto theme `<select>`, restyled by
  tokens. The header is 64px tall.
- **Phone:** the title and the search icon, 56px tall. A splash page has no
  menu, so the landing page carries its own navigation: "Read the quickstart"
  under the hero's command, and the quickstart in the closing band.

**Hero: an override of `Hero.astro`.** I override it rather than putting the
hero in the page body, because with no `hero` in the frontmatter, `Page.astro`
puts `PageTitle` in its own panel above the body, divided by a hairline, and
the hero could not hold the install switcher.

The override reads `hero.title` (HTML, so the second sentence can sit in a
gray `.dim` span) and `hero.tagline`, and the page's own `release` field, a
line on what the latest release added. The version beside it comes from
`plugin/.claude-plugin/plugin.json`, which each release bumps. It must keep
`id={PAGE_TITLE_ID}` and `data-page-title` on the `<h1>`. Upgrade cost:
re-check those two, and the `data.hero` shape, on each Starlight release. The
page body stays MDX, with `not-content` on our sections.

**The page, top to bottom** (decided with Sarvesh from the mockup at
claude.ai/artifact/5T1hjf3hHgKA7teFZneyRE):

1. **Hero, centred.** The release line, the headline "You think very hard.
   decide answers.", a tagline that names the small calls a script makes
   on every commit, ticket and command (the general LLM, not the reader,
   is what it is faster than), "Why not just call an LLM? →" on its own
   line in the hero's link style, and `Install.astro` (brew, go
   install, go get, claude code). Then "Read the quickstart →", the page's
   way into the docs.
2. **`RunStage.astro`.** Five recordings in one window, picked by file tabs
   in its chrome: `triage-check` (issue.md), `review-gate` (change.diff),
   `type-choice` (ticket.txt), `start-line` (review.txt), `type-score`
   (total.go). Only the chosen one shows. The window is 778px and as tall as
   the tallest. The caption's time is read at build time from the summary line
   of the triage-check transcript the page loads, named as the issue.md run
   and called one recording; no hard-coded figure.
3. **"LLMs write. decide answers."** `Compare.astro` (anchor `#compare`, linked
   from the hero's "Why not just call an LLM?"): three 40px faces (ask, lost,
   glad; aria-hidden) over a `<table>` of a general LLM with structured output against decide,
   five rows; decide's column has the answer color on its edge. Below 720px
   each row keeps its two cells side by side under its label, the head row
   is hidden and each cell carries its column name in small mono. The LLM cells
   stay fair: logprobs and an 'unsure' option are named. The price row's footnote cites public prices with
   URLs in a code comment; no multipliers and no output-cost claim for
   OpenAI or Cloudflare. Both price cells name the prompt; the difference is
   output. The credit to LLMs for writing and reasoning sits
   right after the table, the footnote after it.
4. **"The small calls inside your automations."** `WhereItRuns.astro`: CI
   gate, pre-command hook, queue triage, batch scan, each with a command,
   one line and a link to a recipe or tutorial. Under 600px the cells lose
   their borders and stack with gaps.
5. **"Sure ones run. Close calls go to a person."** `UseIt.astro`:
   `triage.go` branches three ways (allow, reject, `gate.Review`), with a
   strip in the pass, unsure and flag colors (three columns, stacked under
   600px; "a person decides" is the widest and set in sans). Right under the
   strip, in 16px body text, the line that the calls that matter stay with
   your team. Then the CI step card. One fine line says the probability is
   the model's judgment and links the calibrate example to compare on
   labeled items before switching. Every band from Compare down is left
   aligned.
6. **"Four decision models, one set of commands."** `Models.astro`: one row
   of four (two by two under 860px), then the line on where your text goes,
   linking to the providers reference.
7. **"No black box. Except the model."** `Source.astro`: no box; the clone
   command, then Apache 2.0 in one sentence with LICENSE and CONTRIBUTING.md
   linked inline.
8. **`Closing.astro`.** "Your turn. Ask it something." One command, the
   quickstart and the source, with the glad-card face (hidden under 600px,
   where the hero's face is enough).

**Positioning:** decide is for the many small calls inside automations, not
the decisions people make. People appear only as the ones who take the close
calls and the ones that matter; never call them slow or replaceable. Against
general LLMs, argue from the mechanism (one value and a probability, so less
to generate, pay for, wait on and parse), never from an invented figure.
Don't sneer at LLMs.

Sections open with a centred two-tone headline; their content is left
aligned. Under 600px a command scrolls inside its box rather than breaking
mid word.

**Copy rules:** say who the key comes from; never say "no account", since a
key needs one; answer "why not JSON mode" where decide answers (the
Compare table).

**`landing.tape`** no longer plays on the page. It stays for the README's GIF
(`Output landing.gif`).

## Recordings, transcripts and frames

**Transcripts.**

- Each tape also writes `Output <tape>.txt`. `record.sh` keeps the last frame's
  text and uploads it as `<tape>-<hash>.txt` beside the MP4 and PNG.
- `Terminal.astro` fetches it at build time. It renders an **"Output as text"**
  `<details>` in the frame, which is selectable and works with JavaScript off
  (R-7). The `aria-label` comes from the caption.
- Captions and "you should see" lines quote verdicts and flag rules, not
  numbers. They may add "your percentages may differ a little".

**Frame anatomy:**

- Radius 12px.
- A 36px bar: the folder name in mono 12px `#A4A8AF`, and a Play/Pause text
  button (icon only on phones).
- A 1px progress line at 50% white, driven by `requestAnimationFrame` while
  playing.
- The video fills the screen. Its 18px inset is recorded in it, because a
  browser draws a video's colors a shade off, and a video inside a CSS
  inset shows a box.
- A foot at `#17181C` holding the commands strip (the visible `Type` lines, with
  a copy button) and the transcript disclosure.
- **Dark theme:** a `rgba(255,255,255,.08)` border and an inset top highlight.
- **Light theme:** a 90% `#0B0C0E` border and the shadow
  `0 1px 2px rgba(16,18,24,.06), 0 12px 32px -12px rgba(16,18,24,.32)`.
- **Caption:** under the frame, at 68ch. It opens with a bold claim about the
  last frame, such as "Flagged on destructive and severe."

**Sizes:**

- Tutorials are 1552×912 at 2x, which is 85×22 cells at FontSize 28 and
  LineHeight 1.0 with Padding 36, shown at up to 776px. VHS's line height
  multiplies the font's own, so 1.0 gives rows about 1.33 times the font
  size. `settings.tape` has the formula for other sizes.
- The width comes from
  `:root[data-has-sidebar]:not([data-has-toc]) { --sl-content-width: 48rem }`
  with `tableOfContents: false`. The `data-has-sidebar` part keeps splash pages
  at 67.5rem.
- **Each tape is written for its last frame.** That frame is the poster and the
  reduced-motion still, so it must show what the caption claims. If the output
  runs past 22 rows, the step records less, or ends with
  `decide runs view --top 3`.

**Phones:**

- The video shows at 0.75 of its size, anchored left, cut at the right with a
  28px fade. Text is about 10.5px, and the answers sit in the first ~40 columns.
- The transcript is the legible version. Tap-to-fullscreen is dropped.
- Before merging, check one real 1552×912 frame on a 3x phone.

**Player (one script):**

- One shared IntersectionObserver, with thresholds [0, .25, .5, .75, 1], keeps
  a registry of the frames.
- The frame most in view, if it is at least 50% visible, plays. All the others
  pause.
- A loop holds each result for about 5s, written into the tape as `Sleep 5s`.
- With reduced motion, nothing autoplays: the poster shows with "▶ Play".

**Code blocks (Expressive Code):**

- One custom dark theme with `useStarlightUiThemeColors: false`.
- `styleOverrides.frames.terminalTitlebarDotsOpacity: '0'`, plus a radius of 12
  and a bar of 34px. Shell blocks get `title="Terminal"`.
- The copy button is moved into the bar with CSS. Re-check that on each
  upgrade.
- Blocks over 25 lines collapse with `collapse=`.
- Syntax colors: keyword `#C69CF4`, string `#D7B98E`, comment `#8A8E96`, text
  `#ECEDEF`. None of them is a verdict color.

## Tutorial pages

1. **The title, and a one-line description**, capped at 68ch.
2. **The goal block**, with two cells:
   - **You'll end with:** names the artifact and where it lives, such as "`checked.sh` in your project: it runs a command only when `command-risk` flags nothing". Then the result to expect, as a dark answer line.
   - **You need:** at most three items. One is "An API key: *get one*", which links to `/start/#get-a-key`. That section lists each provider's sign-up and pricing page. We do not claim a free tier.
3. **Steps,** using Starlight's `<Steps>`. Each step has an imperative h2, one
   or two sentences of why, the code, the recording if the command prints
   something, the caption, and a "you should see" line.
4. **A last step, "Check that it works".** The reader runs the result: a safe
   command runs, and `git push --force origin main` is refused with exit 2.
   UC-3 needs a working result, not a recording of one.
5. **What next:** two `LinkCard`s, the next job and the template's reference
   page.

**Other pages:**

- `/start/go/`: `go get`, set a key, one program read from `examples/` (built
  by R-5), and `go run`. Four steps at most.
- `/start/answers/`: the concepts page. It covers the three question types,
  probability, flags and providers, with the same panels as section 2, and
  search indexes it.
- **Reference:** dense, with a table of contents and no recordings.
  - Each flag is an h3 in mono, with one sentence and one example.
  - The index opens with "New here? Start with the quickstart."
  - `/reference/claude-code/` opens with "Claude Code in three steps".

## Motion

| What | How |
| --- | --- |
| Recordings | Play in view, one at a time. Loop with a 5s hold on each result. |
| Meters | CSS `animation-timeline: view()`, `scaleX` from 0, no JavaScript. Without support, or with reduced motion, they show filled. |
| Hover | Color or border, 120ms. No lift or scale. |
| Copy | The icon becomes a check for 1.2s. |
| Everything else | Still. A theme change is instant. |

## What we avoid

- Gradients, glows, glass, gradient text, sparkles, mascots, 3D renders and
  emoji. Two exceptions: the character (see "The character and the
  voice"), always thinking very hard. And
  the fade on the agent's reply in the strip, which shows text running on.
- "AI-powered" copy.
- Bento grids, logo walls, counters and testimonials.
- Traffic-light dots, including Expressive Code's, except on the landing
  page's windows (`TerminalFrame`'s `lights`).
- Decorative bars, and cyan or verdict colors used outside answers.
- Deep Noodle's lime. Next to green it reads as "passed".
- More than one moving recording at a time.
- Hand-typed percentages in prose.

## The character and the voice

**You are the question. decide is the answer.** The site has one
character, a hand-drawn lumpy face (`Doodle.astro`). It stands in for the
reader, who is always thinking very hard. It is never decide.

- **It has no name.** Never write "Drool" or "the doodle" in page text, alt
  text, aria labels, titles, captions or meta tags, and don't invent a
  replacement. Put the reader in its place ("You think very hard, then
  ask"), describe what it does, or say "it" only beside the drawing.
  Decorative faces are `aria-hidden`. "Doodle" and "the character" are fine
  in code and in these docs.
- **Pen rule.** Strokes keep one width at every size, from `--doodle-pen`
  with `vector-effect: non-scaling-stroke`. Presets: `mark` 1.5px
  (16–24px), `small` 2px (32–96px), `poster` 3px (160px and up). Brow and
  mouth at 0.85 of the pen. There is no drip: it thinks, it does not drool.
- **Poses.** Every face keeps the raised brow and the ask face's two eye
  positions. `ask` thinks, with thought dots that rise and swell, and every
  third 2.4s cycle the last dot turns into "?". `lost` has X eyes, and
  `trail` lets the dots trail off into "?". `glad` has ^ ^ eyes, and
  `glad-card` holds a tag with a real answer from a recording. `mark` is
  the head, eyes, brow and one dot, for the footer and favicon. The story
  runs ask (hero, strip panel 1), lost (strip panel 2, 404), glad (strip
  panel 3, closing). At most one face per viewport.
- **Color means an answer.** The UI is near-monochrome greys, code blocks
  included. Cyan (`--ans`) and red (`--ans-flag`) appear only on answer
  values, probabilities and answer bars.
- **Tokens.** The type scale and band spacing live in `theme.css`:
  `--display-1`, `--display-2`, `--eyebrow` and `--space-band`. No inline
  `clamp()` in `.mdx`. Every band opens with a mono `Eyebrow`.

Voice:

1. Put the reader in the asker's seat. Never name the face.
2. "Thinking very hard" appears once per place, four places in all: the
   hero, the strip, the 404 and the footer. The closing band hands the
   reader their turn instead.
3. decide, always lowercase, is the subject of what happens: "decide
   answers", never "this LLM".
4. Plain, concrete words. Name the flag, the env var, the exit code. No
   "AI-powered", "unlock", "supercharge" or emoji.
5. Dry, not wacky: one quiet joke per block, after the fact.
6. The probability is the model's judgment. It is not proof and not a
   confidence the model wrote about itself. Never "confidence score" or
   "accuracy".
7. A line in decide's output format must be one decide could print. No
   invented run results.
8. Short sentences, second person, contractions, one claim per sentence.

## Later

- **Answer-type recordings:** implemented in #74. Three short real runs in
  section 2 share the one-at-a-time player, commands, and text transcripts.
- **Tutorial poster check:** if a step's last frame can't show its claim within
  22 rows, add a taller settings file (1440×1080). Do this only when it happens.
