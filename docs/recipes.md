# Recipes

Workflows and scripts to copy. Each one runs decide where your work
already happens: on a pull request, on a new issue, before a commit, or on
an export. See the [CLI guide](cli.md) for every flag.

- [Review pull requests in GitHub Actions](#review-pull-requests-in-github-actions)
- [Comment on the pull request](#comment-on-the-pull-request)
- [Block a merge](#block-a-merge)
- [Label new issues](#label-new-issues)
- [Other CI systems](#other-ci-systems)
- [Check changes before you commit](#check-changes-before-you-commit)
- [Export results to a spreadsheet](#export-results-to-a-spreadsheet)

## Review pull requests in GitHub Actions

This workflow judges each function a pull request changes with
`code-risk`. Each flagged function shows up as a warning on the pull
request's changes, and the job's summary page lists every answer. The job
passes either way, so the results are advice and never block a merge.

Add your TypeSafe API key as a repository secret named `TYPESAFE_API_KEY`,
then save this as `.github/workflows/decide.yml`:

```yaml
name: decide

on: pull_request

permissions:
  contents: read

jobs:
  code-risk:
    runs-on: ubuntu-latest
    env:
      TYPESAFE_API_KEY: ${{ secrets.TYPESAFE_API_KEY }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0 # the base branch, to diff against

      - name: Install decide
        run: |
          base=https://github.com/deepnoodle-ai/decide/releases/latest/download
          curl -fsSLO "$base/decide_linux_amd64.tar.gz"
          curl -fsSLO "$base/checksums.txt"
          sha256sum --check --ignore-missing checksums.txt
          tar -xzf decide_linux_amd64.tar.gz decide
          sudo mv decide /usr/local/bin/

      - name: Judge the changed functions
        if: env.TYPESAFE_API_KEY != ''
        env:
          BASE: ${{ github.base_ref }}
        run: |
          git diff "origin/$BASE...HEAD" |
            decide run code-risk --each function --format github
```

A few things to know:

- **Pin a version** by replacing `latest/download` with a release's tag,
  such as `download/v0.1.0`, so an upgrade happens when you choose.
- **Choose the template and unit** to suit the repository. `--each hunk`
  judges each block of changed lines on its own, and a
  [template of your own](cli.md#write-your-own-template), committed under
  `.decide/templates`, can ask what your team cares about.
- **Forks.** GitHub does not give secrets to workflows run for pull
  requests from forks, so the `if:` skips the step for them rather than
  failing. Don't switch to `pull_request_target` to get around this: it
  runs with your secrets and can be tricked into running the fork's code.
- **Annotations.** GitHub shows only the first few warnings of each step on
  the pull request. The job summary lists them all.
- **Cost.** Each judged item is one request, or more for a very long one.
  A diff judges only what changed, and decide skips lockfiles and
  generated files. Add `--limit 200` to cap a large pull request.

## Comment on the pull request

To post the results as a comment instead, print a Markdown report and
keep one comment up to date. This needs permission to write pull request
comments, so it does not work for pull requests from forks.

```yaml
permissions:
  contents: read
  pull-requests: write
```

```yaml
      - name: Judge the changed functions
        if: env.TYPESAFE_API_KEY != ''
        env:
          BASE: ${{ github.base_ref }}
          GH_TOKEN: ${{ github.token }}
          PR: ${{ github.event.pull_request.number }}
        run: |
          git diff "origin/$BASE...HEAD" |
            decide run code-risk --each function --format md > decide.md
          if [ -s decide.md ]; then
            gh pr comment "$PR" --body-file decide.md --edit-last --create-if-none
          fi
```

The report lists flagged items first and collapses the rest. When the
pull request changes nothing decide judges, the report is empty and no
comment is posted.

## Block a merge

Once you trust the results, add `--fail-on flagged`. The job then fails
when any item is flagged, and the annotations become errors:

```sh
git diff "origin/$BASE...HEAD" |
  decide run code-risk --each function --format github --fail-on flagged
```

Exit code 2 means something was flagged, and 1 means decide could not
finish, such as when the API key is wrong. Make the job a required check
in the branch's protection rules to block merges on it. An answer close to
being flagged does not count, and a template's `flags` set where the line
is. Before you block on a template, run it on past pull requests with
`--format md` to see how often it flags.

## Label new issues

This workflow labels each new issue `billing` or `engineering` with the
`ticket-routing` template, when the model is at least 70% sure. Create
those labels first.

```yaml
name: label issues

on:
  issues:
    types: [opened]

permissions:
  issues: write

jobs:
  route:
    runs-on: ubuntu-latest
    steps:
      - name: Install decide
        run: |
          base=https://github.com/deepnoodle-ai/decide/releases/latest/download
          curl -fsSLO "$base/decide_linux_amd64.tar.gz"
          curl -fsSLO "$base/checksums.txt"
          sha256sum --check --ignore-missing checksums.txt
          tar -xzf decide_linux_amd64.tar.gz decide
          sudo mv decide /usr/local/bin/

      - name: Choose a queue
        env:
          TYPESAFE_API_KEY: ${{ secrets.TYPESAFE_API_KEY }}
          GH_TOKEN: ${{ github.token }}
          TITLE: ${{ github.event.issue.title }}
          BODY: ${{ github.event.issue.body }}
          NUMBER: ${{ github.event.issue.number }}
        run: |
          queue=$(printf '%s\n\n%s\n' "$TITLE" "$BODY" |
            decide run ticket-routing --each file --json |
            jq -r '.answers.queue | select(.probabilities[.choice] >= 0.7) | .choice')
          if [ -n "$queue" ] && [ "$queue" != "other" ]; then
            gh issue edit "$NUMBER" --repo "$GITHUB_REPOSITORY" --add-label "$queue"
          fi
```

`--each file` makes the whole issue one item; piped text is otherwise
judged line by line. The label can only be one of the template's choices,
whatever the issue says. Pass the issue's text through `env`, as above,
and never write `${{ github.event.issue.body }}` into the script itself,
where an issue could run commands.

To flag urgent issues as well, run the `triage` template the same way.

## Other CI systems

decide needs only a diff and an API key, so the same commands work in
any CI system. In a GitLab merge request pipeline:

```yaml
decide:
  image: alpine:latest
  variables:
    GIT_DEPTH: 0 # the full history, to diff against the target branch
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - apk add --no-cache curl git
    - curl -fsSL https://github.com/deepnoodle-ai/decide/releases/latest/download/decide_linux_amd64.tar.gz | tar -xz -C /usr/local/bin decide
    - git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"
    - git diff "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME...HEAD" | decide run code-risk --each function --format md > decide.md
  artifacts:
    paths: [decide.md]
```

Set `TYPESAFE_API_KEY` as a masked CI/CD variable. Add `--fail-on flagged`
to fail the pipeline instead.

## Check changes before you commit

A git hook can judge your staged changes and stop the commit when one is
flagged. Save this as `.git/hooks/pre-commit` and make it executable:

```sh
#!/bin/sh
git diff --cached | decide run code-risk --each hunk --fail-on flagged
```

Skip the check for one commit with `git commit --no-verify`. The hook
judges each hunk rather than each function, because the file on disk can
differ from what is staged.

## Export results to a spreadsheet

```sh
decide run ticket-routing tickets.jsonl --field body --format csv > routed.csv
decide runs view --format csv > results.csv   # a run you already have
```

Each row is one item, with a column for each question. See
[Output formats](cli.md#output-formats) for what the columns hold.
