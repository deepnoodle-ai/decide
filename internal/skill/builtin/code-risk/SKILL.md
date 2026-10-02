Each file is judged on its own. To judge each function or method instead,
add `--each function`; this works for Go, Python, JavaScript, TypeScript,
and Java, and sends each function with its file's imports and the line that
starts its class.

`risk` is the probability that the code or configuration risks the problems
named by `focus`. `maintainability`
is a score from 0 (hard to follow) to 4 (exceptionally clear).

An item is flagged when it is likely risky, or when its maintainability is
1.5 or lower.

Start with a few files, for example `--limit 5`, before you run a whole
repository.
