// Package rank orders caller-owned candidates from System One Noul
// observations and takes an ordered prefix under a caller-defined allowance.
//
// Rank does not send requests. PairwiseSort needs one observation for every
// unordered pair. Its expected-win order is computed from those values;
// HasCycle reports cycles in strict pairwise preferences. No result claims
// that a model's judgments are accurate or statistically reliable.
package rank
