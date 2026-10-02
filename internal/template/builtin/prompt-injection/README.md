Use it on anything an agent will read: web pages, issues, emails, tool
output, documents, or the changes in a pull request.

    git diff main | decide run prompt-injection
    decide run prompt-injection pages --each section

`injection` is the probability that the content tries to make an AI agent
act against its user, such as by ignoring its instructions or sending data
away. A project's own instructions for its agents, like `AGENTS.md`, do not
count. `hidden` is the probability that some of the text is hidden from
people but not from programs, such as in an HTML comment or zero-width
characters.

An item is flagged when either one is likely. To block a pull request on
it, add `--fail-on flagged`.
