// Package openai connects decide to OpenAI's Decisions API, which runs on
// gpt-6-luna. The API is in public beta.
//
// NewTransport supplies a decide.Transport. Use it with decide.WithTransport
// and decide.WithModel("gpt-6-luna"). The transport translates each request to
// POST /v1/decisions and each answer back to decide's types: a noul question
// is sent as a predicate, a choice as a choice, and a score as a score. The
// decide.Client owns retries and answer validation. Models.List is
// unsupported.
//
// The Decisions API has no criteria field for a predicate, so a noul
// question's NoulTrue and NoulFalse descriptions are added to its
// instructions. Choice option descriptions and score levels are sent as
// text; a value that is not a string is sent as its JSON. Non-string state is
// sent as its JSON text. A refusal comes back as an answer of type "refusal",
// which the client reports as invalid for that question.
//
// Requests are checked against the API's limits before they are sent: at
// most 200 questions, and at least 2 options per choice and 2 levels per
// score. Image input is not supported yet.
//
// Config reads no environment variables. The API key is redacted from
// formatting and response bodies. Malformed and non-JSON response bodies are
// suppressed, since they cannot be safely inspected for escaped credentials.
// Like decide.Request, a request must not be mutated while a call uses it.
//
// Guide: https://developers.openai.com/api/docs/guides/decisions
package openai
