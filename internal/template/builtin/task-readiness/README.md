Each record is one task: a line of JSONL, an element of a JSON array, a
row of CSV, or a Markdown or text file. To check your open GitHub issues:

    gh issue list --json number,title,body | decide run task-readiness

`ready` is the probability that the task can be done without asking
anyone a question: it says what should change, where, and how to tell
when it is done. `size` is small, medium, or large, where large is too big
for one pull request.

A task is flagged when it is likely not ready, or when it is large. To
judge readiness for someone else, set who does the work:

    decide run task-readiness tasks.jsonl -p assignee="a new engineer"
