// Package pick asks a decision model to select an item from a caller's
// candidate list and returns the original item.
//
// A Picker copies the candidate list and derives unique option keys. It
// supports strings, fmt.Stringer values, and arbitrary values with custom
// descriptions or keys. Each question includes an abstain option. When that
// option wins, Result.Picked is false and no item is returned.
//
// Results expose the item's index, probability, abstain probability,
// runner-up key, and margin. The underlying ChoiceAnswer remains available.
//
// Handle.From follows decide.Handle.From: an answer that fails only a
// consistency check returns a full Result together with an error matching
// decide.ErrInconsistentAnswer, so a caller can choose to accept it.
package pick
