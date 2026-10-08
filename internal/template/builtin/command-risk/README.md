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
sending a file to another machine. `publish` is the probability that it
puts something in front of users or changes a live system, such as
deploying, publishing a package or release, or sending a message.
Pushing a branch or opening a pull request is not publishing. Pushing a
release tag and setting a secret score near the flag, since whether they
publish depends on what the repository's CI does with them.
`severe` is the probability that it causes severe harm beyond the local
checkout that is hard to undo, such as deleting a home folder,
force-pushing a shared branch, dropping a database, destroying cloud
resources, or sending secrets to another machine. Discarding changes in a
local checkout, such as `git checkout -- .` or `git clean -fdx`, is
destructive but not severe.

A command is flagged when any question is at least 80% likely. To flag
only the worst commands, read `severe` alone, as the plugin does.
