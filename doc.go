// Package decide is a Go client for decision models: TypeSafe's Jev,
// Cloudflare's Clef, and OpenAI's GPT-6 Luna. TypeSafe calls these System
// One models.
//
// A decision model evaluates a piece of state (a ticket, a diff, a JSON
// record) against a set of typed questions and returns typed answers with
// probabilities that code can act on directly. It does not generate text.
// [NewClient] connects to TypeSafe; the backend package connects to any
// provider through the same API. The templates package holds the decide
// command's built-in questions.
//
// [Eval] asks one typed question and returns an [Evaluation]. [Pick]
// selects an original candidate or abstains, returning a [Decision]. Both
// retain response evidence and apply no application thresholds.
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
//	client, err := decide.NewClient() // reads TYPESAFE_API_KEY
//	if err != nil {
//		return err
//	}
//	req := decide.NewRequest(ticketText)
//	billing := decide.Ask(req, "billing", decide.Noul("Is this ticket about billing?"))
//	tone := decide.Ask(req, "tone", decide.Choice("What is the customer's tone?",
//		decide.Option("calm"), decide.Option("frustrated"), decide.Option("angry")))
//
//	resp, err := client.SystemOne(ctx, req)
//	if err != nil {
//		return err
//	}
//	b, _ := billing.From(resp) // *decide.NoulAnswer
//	t, _ := tone.From(resp)    // *decide.ChoiceAnswer
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
// The decidetest package provides a fake server for tests and examples.
package decide
