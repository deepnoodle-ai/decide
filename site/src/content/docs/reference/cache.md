---
title: The answer cache
description: decide keeps every answer and asks again only about what changed.
---

decide keeps every answer it gets, and asks again only about what changed.
Run a template a second time over the same files and nothing is sent:

```
✓ 41212 answered  ! 3 flagged  48s
  81974 answers from cache · 450 asked
```

The line counts answers, one for each question about each item, so an
item can have some answers from the cache and others asked.

An answer is reused only for the same question about the same text, sent
to the same provider and address, with the same model name. Each question
is kept on its own, so adding a question to a template asks only that one.
A question is known by its key and its full definition, so changing its
wording or options, or renaming its key, asks it again. Flags and
`--fail-on` are worked out from the answers on each run, so changing a
threshold asks nothing again.

With `--json`, each result lists in `cached` the questions whose answers
came from the cache. `request_id` and `model` describe the request the run
sent for the item, and are left out when every answer came from the
cache.

`--dry-run` says how many answers the cache already holds:

```
41212 items · 81974 answers in the cache · 450 to ask
```

The cache is keyed by the model name you ask for, such as `jev-latest` or
`clef`. Each answer also notes the model version that gave it, when the
provider names one. When a live answer shows that the model behind the
name changed, older answers are asked again, including the cached answers
of the item that showed it. A run where everything is cached sends no
request, so it can't see an upgrade: run with `--no-cache` to ask fresh,
or name an exact version, such as `--model jev-1.13.0`, to pin answers to
it. Workers AI names no version, so Clef answers stay until `--no-cache`.

`--no-cache` asks every question again, and keeps the new answers in place
of the old. `runs resume` keeps the setting of the run it resumes.

The cache is the folder `~/.decide/cache`, readable only by you. It holds
hashes and answers, never your text, file names, or images. Several decide
processes can use it at once. Delete the folder to clear it. If it can't be
read or written, decide warns and asks every question.
