Each file is judged on its own. `risk` is the probability that the file's
code or configuration risks the problems named by `focus`. `maintainability`
is a score from 0 (hard to follow) to 4 (exceptionally clear).

A file is flagged when it is likely risky, or when its maintainability is
1.5 or lower.

Start with a few files, for example `--limit 5`, before you run a whole
repository.
