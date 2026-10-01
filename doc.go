// Package sod is a Go client for TypeSafe AI's System One models, such
// as Jev.
//
// A System One model evaluates a piece of state (a ticket, a diff, a JSON
// record) against a set of typed questions and returns typed answers with
// probabilities that code can act on directly. It does not generate text.
//
// Three primitives cover most questions:
//
//   - [Noul]: does a condition hold? The answer is P(yes) in [0,1].
//   - [Choice]: which one of these options? The answer is the chosen option,
//     the full distribution, and a confidence.
//   - [Score]: where on this ordered scale? The answer is the
//     probability-weighted level, the distribution, and a confidence.
//
// Build a [Request], add questions with [Ask] to get typed handles, send it
// with [Client.SystemOne], and read answers back through the handles:
//
//	client, err := sod.NewClient() // reads TYPESAFE_API_KEY
//	if err != nil {
//		return err
//	}
//	req := sod.NewRequest(ticketText)
//	billing := sod.Ask(req, "billing", sod.Noul("Is this ticket about billing?"))
//	tone := sod.Ask(req, "tone", sod.Choice("What is the customer's tone?",
//		sod.Option("calm"), sod.Option("frustrated"), sod.Option("angry")))
//
//	resp, err := client.SystemOne(ctx, req)
//	if err != nil {
//		return err
//	}
//	b, _ := billing.From(resp) // *sod.NoulAnswer
//	t, _ := tone.From(resp)    // *sod.ChoiceAnswer
//	fmt.Println(resp.Model, b.Noul, t.Choice, t.Confidence)
//
// Every answer is checked against its question before it is returned: the
// keys match, required fields are present, a choice is one of the offered
// options, probabilities are finite and in range. On a failure SystemOne
// returns the response together with an [*InvalidAnswersError], and
// [Response.Invalid] names the keys that failed; the other keys stay usable.
// Failures where the numbers merely disagree beyond tolerance (the choice is
// not the argmax, probabilities do not sum to about 1) also match
// [ErrInconsistentAnswer], so a caller can choose to accept them.
//
// Transient failures (408, 429, 5xx, and connection errors) are retried with
// backoff that honors Retry-After. Errors from the API are [*APIError]
// values that match sentinels such as [ErrRateLimited] and [ErrAuth] with
// [errors.Is] and carry the request ID.
//
// Question and answer types are open. A question type from another package
// decodes into its own answer type when it implements [AnswerMaker]; answer
// types can also be registered with [RegisterAnswerType]; and an answer type
// nobody knows decodes to a [*RawAnswer] instead of failing the response.
// [RawQuestion] sends a question type this package does not model.
//
// The sodtest package provides a fake server for tests and examples.
package sod
