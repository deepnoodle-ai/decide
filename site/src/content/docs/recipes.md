---
title: Recipes
description: Workflows and scripts to copy, for CI systems, git hooks, issues, and spreadsheets.
---

Workflows and scripts to copy. Each one runs decide where your work already
happens. The tutorials walk through the three most common from the start:

- [Review pull requests](/tutorials/review-pull-requests/) in GitHub
  Actions, and block a merge.
- [Triage new issues](/tutorials/triage-issues/) as they're opened.
- [Check a command before an agent runs it](/tutorials/check-agent-commands/).

## Run decide in GitHub Actions

The [pull request workflow](/tutorials/review-pull-requests/#run-it-on-each-pull-request)
runs as it is. A few things to know before you rely on it:

- **Pin a version** by replacing `latest/download` with a release's
  tag, such as `download/v0.3.0`, so an upgrade happens when you choose.
- **The cache** keeps decide's [answers](/reference/cache/) between
  pushes, so a function judged on an earlier push isn't asked about
  again. A pull request's first push starts from its base branch's
  cache. The key starts with `github.ref`, so a pull request never
  restores a cache that a fork's pull request saved. The base branch's
  cache is saved by a workflow that runs on pushes to it.
- **Forks.** GitHub gives no secrets to pull requests from forks, so the
  `if:` skips the step for them. Don't switch to `pull_request_target`
  to get around this: it runs with your secrets and can be tricked into
  running the fork's code.
- **Cost.** Each judged function is one request. decide skips lockfiles
  and generated files. Add `--limit 200` to cap a large pull request.
- **Annotations.** GitHub shows at most 10 for each step. The job
  summary lists up to 100 items in each table.

## Comment on the pull request

To post the results of the [pull request workflow](/tutorials/review-pull-requests/#run-it-on-each-pull-request)
as a comment instead, print a Markdown report and keep one comment up to
date. This needs permission to write pull request
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
          marker='<!-- decide code-risk -->'
          echo "$marker" > decide.md
          git diff "origin/$BASE...HEAD" |
            decide run code-risk --each function --format md >> decide.md
          id=$(gh api "repos/$GITHUB_REPOSITORY/issues/$PR/comments" --paginate \
            --jq ".[] | select(.body | startswith(\"$marker\")) | .id" | head -n 1)
          if [ -n "$id" ]; then
            gh api -X PATCH "repos/$GITHUB_REPOSITORY/issues/comments/$id" -F body=@decide.md
          else
            gh pr comment "$PR" --body-file decide.md
          fi
```

The first line of the comment is a hidden marker, so each push updates
the same comment, whatever else comments on the pull request. The report
lists flagged items first and collapses the rest. When a push leaves
nothing for decide to judge, the report says so.

## Route new issues to a queue

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

To label the issues that aren't ready to work on, see
[Triage new issues](/tutorials/triage-issues/). To flag urgent ones, run
the `triage` template the same way.

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
    - base=https://github.com/deepnoodle-ai/decide/releases/latest/download
    - curl -fsSLO "$base/decide_linux_amd64.tar.gz" -O "$base/checksums.txt"
    - grep ' decide_linux_amd64.tar.gz$' checksums.txt | sha256sum -c -
    - tar -xzf decide_linux_amd64.tar.gz -C /usr/local/bin decide
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
[Output formats](/reference/output/) for what the columns hold.
