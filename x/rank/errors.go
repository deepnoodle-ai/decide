package rank

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidInput       = errors.New("rank: invalid input")
	ErrInvalidObservation = errors.New("rank: invalid observation")
	ErrIncompletePairs    = errors.New("rank: incomplete pairwise comparisons")
	ErrDuplicatePair      = errors.New("rank: duplicate pairwise comparison")
)

// InputError identifies the affected candidate or pair. A negative index
// means that index does not apply. Kind supports errors.Is.
type InputError struct {
	Kind   error
	Index  int
	Other  int
	Detail string
}

func (e *InputError) Error() string {
	if e.Other >= 0 {
		return fmt.Sprintf(
			"%v at pair (%d,%d): %s",
			e.Kind,
			e.Index,
			e.Other,
			e.Detail,
		)
	}
	if e.Index >= 0 {
		return fmt.Sprintf("%v at item %d: %s", e.Kind, e.Index, e.Detail)
	}
	return fmt.Sprintf("%v: %s", e.Kind, e.Detail)
}

func (e *InputError) Unwrap() error { return e.Kind }

func inputError(kind error, index, other int, detail string) error {
	return &InputError{Kind: kind, Index: index, Other: other, Detail: detail}
}
