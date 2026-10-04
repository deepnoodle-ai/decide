Use it on shell commands before they run: one command per line of text,
or one per JSON record with `--field`. The decide plugin for Claude Code
runs it before each command Claude runs.

    echo 'git reset --hard HEAD~3' | decide run command-risk
    decide run command-risk commands.txt --fail-on flagged

`destructive` is the probability that the command destroys work or data
that is hard to get back, such as deleting files, discarding uncommitted
changes, force-pushing, or dropping a database. `leak` is the probability
that it exposes secrets or private data, such as by printing a key or
sending a file to another machine. `external` is the probability that it
acts where others can see, such as pushing, publishing, or deploying.

A command is flagged when `destructive` or `leak` is at least 80% likely.
`external` is never flagged: pushing a branch or opening a pull request is
routine for an agent.
