---
title: Providers
description: The four decision models decide runs on, and how to point it at your own service.
---

decide runs on four decision models, through the same commands and
templates:

- **Jev** by [TypeSafe](https://docs.typesafe.ai/introduction), the
  default, with the `jev-latest` model:

  ```sh
  export TYPESAFE_API_KEY=...
  ```

- **Clef** by [Cloudflare Workers AI](https://developers.cloudflare.com/workers-ai/models/clef/),
  with the `clef` model, or `clef-flash` for lower latency. Image templates
  always use Clef.

  ```sh
  export CLOUDFLARE_AUTH_TOKEN=...
  export CLOUDFLARE_ACCOUNT_ID=...
  export DECIDE_PROVIDER=cloudflare
  ```

- **GPT-6 Luna** by [OpenAI](https://developers.openai.com/api/docs/guides/decisions),
  with the `gpt-6-luna` model, through the Decisions API, which is in beta.
  Set `OPENAI_BASE_URL` to send requests somewhere other than
  `https://api.openai.com/v1`. Image templates don't run on it yet.

  ```sh
  export OPENAI_API_KEY=...
  export DECIDE_PROVIDER=openai
  ```

- **Gemini Decision** by [Google](https://aistudio.google.com/docs/decisions-api),
  with the `gemini-decision-1` model, through the Gemini Decisions API,
  which is in beta. Set `GEMINI_BASE_URL` to send requests somewhere other
  than `https://generativelanguage.googleapis.com/v1beta`. Image templates
  don't run on it yet.

  ```sh
  export GEMINI_API_KEY=...
  export DECIDE_PROVIDER=gemini
  ```

Any other service that speaks the Jev API works too, such as one you host
yourself. Point decide at it with `TYPESAFE_BASE_URL`, along with
`TYPESAFE_API_KEY` and a model name:

```sh
export TYPESAFE_BASE_URL=https://decisions.example.com
decide run sentiment notes.txt --model my-model
```

Choose for one run with `--provider typesafe|cloudflare|openai|gemini` and `--model NAME`,
or for every run with `DECIDE_PROVIDER` and `DECIDE_MODEL`. `--workers` sets how many
requests run at once (default 4).

Check your setup with `decide models`. It checks that the provider accepts the key or
token, and lists the model names you can pass to `--model`. See
[`decide models`](/reference/cli/#decide-models).

Each provider names its own model versions. The [answer cache](/reference/cache/)
keeps answers for each provider, address, and model name apart.
