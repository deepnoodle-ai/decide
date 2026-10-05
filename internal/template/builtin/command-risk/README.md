Use it on shell commands before they run: one command per line of text,
or one per JSON record with `--field`. The decide plugin for Claude Code
runs it before each command Claude runs in bypass mode, and acts only on
`severe`.

    echo 'git reset --hard HEAD~3' | decide run command-risk
    decide run command-risk commands.txt --fail-on flagged

`destructive` is the probability that the command destroys work or data
that is hard to get back, such as deleting files, discarding uncommitted
changes, force-pushing, or dropping a database. `leak` is the probability
that it exposes secrets or private data, such as by printing a key or
sending a file to another machine. `external` is the probability that it
acts where others can see, such as pushing, publishing, or deploying.
`severe` is the probability that it causes severe harm beyond the local
checkout that is hard to undo, such as deleting a home folder,
force-pushing a shared branch, dropping a database, destroying cloud
resources, or sending secrets to another machine. Discarding changes in a
local checkout, such as `git checkout -- .` or `git clean -fdx`, is
destructive but not severe.

A command is flagged when `destructive`, `leak`, or `severe` is at least 80%
likely. `external` is never flagged: pushing a branch or opening a pull
request is routine for an agent. To flag only the worst commands, read
`severe` alone, as the plugin does.
