// Package cloudflare connects sod to the Clef and Clef-flash models on
// Cloudflare Workers AI. It is experimental.
//
// NewTransport supplies a sod.Transport. Use it with sod.WithTransport and
// sod.WithModel("clef") or sod.WithModel("clef-flash"). The transport handles
// the Workers AI routes and envelope. The sod.Client owns retries and answer
// validation. Models.List is unsupported.
//
// SetImages adds embedded PNG, JPEG, or WebP images to a request. Remote
// image URLs and video inputs are unsupported by the hosted API. Cloudflare
// documents server-side truncation of long text state to the context limit.
// This package does not shorten state itself.
//
// Config reads no environment variables. Use a Workers AI API token and
// account ID. Credentials are redacted from formatting and response bodies.
// Malformed and non-JSON response bodies are suppressed, since they cannot
// be safely inspected for escaped credentials.
// Like sod.Request, a request must not be mutated while a call uses it.
//
// Schemas: https://developers.cloudflare.com/workers-ai/models/clef/
package cloudflare
