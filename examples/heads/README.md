# examples/heads

```sh
TYPESAFE_API_KEY=... go run ./examples/heads
```

One request asks which team owns a ticket plus each team's follow-up
question. Only the chosen team's answer is read. A question in one request
cannot see the selector's answer, so each follow-up states its premise
("If this is billing, ..."). If the chosen answer is needed to fetch
evidence or build a new question, make a second request.
