// Package gate turns System One answers into one of three outcomes: Allow,
// Review, or Escalate. What review and escalate do is the caller's job.
//
// Thresholds must be measured on your own data. This package ships no
// numbers and no default measure. Once thresholds are tuned, pin a
// versioned model ID (for example "jev-1.13.0"), because an alias such as
// "jev-latest" can move without any change on your side. Nothing here
// claims that gating makes a system safer or more accurate.
//
// # Use
//
// Adapt answers to [Inputs], then evaluate a [Rule]:
//
//	resp, err := client.SystemOne(ctx, req) // log err; resp may still be usable
//	d := policy.Evaluate(gate.FromResponse(resp))
//	switch d.Outcome {
//	case gate.Allow:
//		run()
//	default:
//		handOff(d.Rule, d.Weakest, d.Reason)
//	}
//
// The built-in rules are [Bands], [AllOf], [MinConfidence], [Margin],
// [Asymmetric], and [Compose]. Each names the measure it reads (see
// [Measure]); none has a default. An input that is missing, failed,
// abstained, undefined for the measure, or out of range yields the rule's
// OnMissing outcome, and an unset outcome field means Escalate, so a
// forgotten field fails closed. Built-in rules round-trip through JSON with
// [DecodeRule]. Custom rules implement [Rule].
//
// A consistency failure from the client (decide.ErrInconsistentAnswer) is
// Failed by default; pass [AcceptInconsistent] to read such answers anyway.
package gate
