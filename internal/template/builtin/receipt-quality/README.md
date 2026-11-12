Each PNG, JPEG, or WebP image is one item. Image templates run on
Cloudflare, OpenAI, or Gemini. With no `--provider` or `DECIDE_PROVIDER`,
decide uses the first of these whose credentials are set:
`CLOUDFLARE_AUTH_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`, `OPENAI_API_KEY`, or
`GEMINI_API_KEY`. Choose one with `--provider cloudflare`,
`--provider openai`, or `--provider gemini`. A receipt saved as a PDF works
too when you name it, on Gemini: `decide run receipt-quality receipt.pdf
--provider gemini`.
