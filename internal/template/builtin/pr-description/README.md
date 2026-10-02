Each record is one pull request: a JSON object with its title and body, an
element of a JSON array, a line of JSONL, or a Markdown file. To
check a pull request, or your open ones:

    gh pr view 42 --json number,title,body | decide run pr-description
    gh pr list --json number,title,body | decide run pr-description

`title` is the probability that the title says what the change does. `why`
is the probability that the description says why the change is needed, and
`tested` that it says how the change was tested or can be checked. A typo,
docs, or dependency update needs no testing notes. `guidelines` is the
probability that the pull request follows your team's guidelines. By
default, the title is ten words or fewer in the imperative mood, and may
start with a prefix such as `fix(cli): `.

A pull request is flagged when any answer is likely no. To use your own
guidelines, set them as a sentence or two:

    decide run pr-description prs.json -p guidelines="The title follows Conventional Commits, such as fix(cli): ..."

To fail a CI job when the description falls short, add `--fail-on flagged`;
decide then exits with code 2.
