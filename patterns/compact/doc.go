// Package compact shortens a long context by asking, for each segment,
// whether it is still needed, then keeping the highest-scoring segments
// verbatim under a budget. Nothing is summarized: a segment is kept whole,
// replaced by a short form the caller wrote in code, or dropped.
//
// Select is pure and exported so a caller can run a recency baseline or
// their own scores through the same budget logic.
//
// Design: https://github.com/deepnoodle-ai/decide/blob/main/docs/prds/decision-tools.md
package compact
