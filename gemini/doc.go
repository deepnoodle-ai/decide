// Package gemini connects decide to the Gemini Decisions API, which runs on
// gemini-decision-1. The API is in beta, on the Gemini Developer API only.
//
// NewTransport supplies a decide.Transport. Use it with decide.WithTransport
// and decide.WithModel("gemini-decision-1"). The transport sends each request
// to POST /v1beta/decisions. Gemini's noul, choice, and score questions are
// decide's own, so translation is small: a noul question's criteria become
// true_description and false_description, and a score's legend keeps the
// levels as they were asked. The decide.Client owns retries and answer
// validation. Models.List is unsupported.
//
// Instructions, option descriptions, and score levels are sent as text; a
// value that is not a string is sent as its JSON. Non-string state is sent
// as its JSON text.
//
// Requests are checked against the API's limits before they are sent: 1 to
// 1,000 questions, keys of at most 64 letters, digits, "_" or "-", 2 to 255
// options per choice, and 2 to 255 non-empty levels per score.
//
// The transport is a decide.MediaTransport. It sends images, audio, video,
// and PDFs attached with decide.WithMedia, as bytes or by HTTPS URL, such as
// a Files API URI: the state as a text block, then one block per file. It
// rejects YouTube URLs and MIME types the API does not list with
// errors.ErrUnsupported before any network call. Inline files may be at
// most 100 MB of base64, or 50 MB when a PDF is inline; a PDF by URL does
// not lower the limit.
//
// Google answers an invalid API key with status 400. The transport reports
// it as a *decide.APIError that also matches decide.ErrAuth. A safety block
// is a 400 whose APIError.Type is the block's code, such as "safety" or
// "prohibited_content".
//
// Config reads no environment variables. The API key is redacted from
// formatting and response bodies. Malformed and non-JSON response bodies are
// suppressed, since they cannot be safely inspected for escaped credentials.
// Like decide.Request, a request must not be mutated while a call uses it.
//
// Guide: https://aistudio.google.com/docs/decisions-api
package gemini
