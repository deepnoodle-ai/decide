---
title: Providers
description: The three decision models decide runs on, and how to point it at your own service.
---

decide runs on three decision models, through the same commands and
templates:

- **Jev** by [TypeSafe](https://docs.typesafe.ai/introduction), the
  default, with the `jev-latest` model:

  ```sh
  export TYPESAFE_API_KEY=...
  ```

- **Clef** by [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/models/clef/),
  with the `clef` model, or `clef-flash` for lower latency. Image templates
  use Clef by default, and need it: another provider is an error.

  ```sh
  export CLOUDFLARE_AUTH_TOKEN=...
  export CLOUDFLARE_ACCOUNT_ID=...
  export DECIDE_PROVIDER=cloudflare
  ```

  Set `CLOUDFLARE_BASE_URL` to send requests somewhere other than Workers AI.

- **GPT-6 Luna** by [OpenAI](https://developers.openai.com/api/docs/guides/decisions),
  with the `gpt-6-luna` model, through the Decisions API, which is in beta.
  Set `OPENAI_BASE_URL` to send requests somewhere other than
  `https://api.openai.com/v1`. Image templates don't run on it yet.

  ```sh
  export OPENAI_API_KEY=...
  export DECIDE_PROVIDER=openai
  ```

Any other service that speaks the Jev API works too, such as one you host
yourself. Point decide at it with `TYPESAFE_BASE_URL`, along with
`TYPESAFE_API_KEY` and a model name:

```sh
export TYPESAFE_BASE_URL=https://decisions.example.com
decide run sentiment notes.txt --model my-model
```

Choose for one run with `--provider typesafe|cloudflare|openai` and `--model NAME`,
or for every run with `DECIDE_PROVIDER` and `DECIDE_MODEL`. `--workers` sets how many
requests run at once (default 4).

## Limits

decide checks a request against the provider's limits before it sends it.

| | Clef | GPT-6 Luna |
| --- | --- | --- |
| Questions per request | 64 | 200 |
| Question IDs | up to 100 ASCII letters, digits, `_`, `.` or `-` | |
| Options per `choice` | 2 to 255 | at least 2 |
| Levels per `score` | 2 to 10 | at least 2 |
| Images | four per request, 4 MiB and 16 megapixels each, 8 MiB decoded combined, 13 MiB per request | not supported yet |

Each provider names its own model versions. The [answer cache](/reference/cache/)
keeps answers for each provider, address, and model name apart.
