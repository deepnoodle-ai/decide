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
  with the `clef` model, or `clef-flash` for lower latency. It takes
  images of up to 4 MiB and 16 megapixels each.

  ```sh
  export CLOUDFLARE_AUTH_TOKEN=...
  export CLOUDFLARE_ACCOUNT_ID=...
  export DECIDE_PROVIDER=cloudflare
  ```

  Set `CLOUDFLARE_BASE_URL` to send requests somewhere other than Workers AI.

- **GPT-6 Luna** by [OpenAI](https://developers.openai.com/api/docs/guides/decisions),
  with the `gpt-6-luna` model, through the Decisions API, which is in beta.
  It takes images of up to 50 MiB each. Set `OPENAI_BASE_URL` to send
  requests somewhere other than `https://api.openai.com/v1`.

  ```sh
  export OPENAI_API_KEY=...
  export DECIDE_PROVIDER=openai
  ```

- **Gemini Decision** by [Google](https://aistudio.google.com/docs/decisions-api),
  with the `gemini-decision-1` model, through the Gemini Decisions API,
  which is in beta. It is the only provider for audio, video, and PDF
  templates, and it runs image templates too. It takes files of up to
  about 70 MiB, or about 35 MiB for a PDF. Set `GEMINI_BASE_URL` to send
  requests somewhere other than
  `https://generativelanguage.googleapis.com/v1beta`.

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

An image template with no `--provider` runs on the first of Cloudflare,
OpenAI, and Gemini whose credentials are set, and an audio, video, or PDF
template runs on Gemini.

Choose for one run with `--provider` and `--model`, or for every run with
`DECIDE_PROVIDER` and `DECIDE_MODEL`. Check your setup with
[`decide models`](/reference/cli/#decide-models).
