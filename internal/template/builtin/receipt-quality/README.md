Each PNG, JPEG, or WebP image is one item. Image templates use the
Cloudflare provider by default, so set `CLOUDFLARE_AUTH_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID` first. They also run on OpenAI with
`--provider openai` and `OPENAI_API_KEY`, and on Gemini with
`--provider gemini` and `GEMINI_API_KEY`. With no `--provider`, decide uses
the first of these whose credentials are set.
