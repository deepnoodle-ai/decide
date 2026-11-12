---
title: The answer cache
description: decide keeps every answer and asks again only about what changed.
---

decide keeps every answer it gets, and asks again only about what changed.
Run a template a second time over the same files and only the new or changed
items are asked:

```
✓ 41212 answered  ! 3 flagged  48s
  81974 answers from cache · 450 asked
```

An answer is reused only for the same question about the same text, from
the same provider and model name. Each question is kept on its own:

- Adding a question to a template asks only that one.
- Changing a question's wording, options, or key asks it again.
- Changing a flag's threshold asks nothing.

`--dry-run` says how many answers the cache already holds:

```
41212 items · 81974 answers in the cache · 450 to ask
```

## When the model changes

Answers are kept by the model name you ask for, such as `jev-latest`. When
decide sees that the model behind a name has changed, it asks again. To ask
fresh yourself, add `--no-cache`. To keep answers from one version, name
it, such as `--model jev-1.13.0`. Workers AI names no version, so Clef
answers stay until `--no-cache`.

## Where it lives

The cache is the folder `~/.decide/cache`, or `cache` inside `DECIDE_HOME`
if you set it. It holds hashes and answers, never your text, file names, or
images. Delete the folder to clear it.
