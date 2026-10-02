package pick

import (
	"errors"
	"fmt"
)

// Sentinel errors. Match them with errors.Is.
var (
	ErrNoItems        = errors.New("pick: no items")
	ErrTooManyItems   = errors.New("pick: too many items for one choice")
	ErrInvalidKey     = errors.New("pick: invalid key")
	ErrInvalidOption  = errors.New("pick: invalid option")
	ErrUnmappedChoice = errors.New("pick: choice does not map to an item")
)

// narrowingURL documents the two-stage workaround for lists past the cap.
const narrowingURL = "https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook"

// TooManyItemsError reports that the items plus the abstain option exceed
// the option cap. Options is always Items+1.
type TooManyItemsError struct{ Items, Options, Max int }

func (e *TooManyItemsError) Error() string {
	return fmt.Sprintf("pick: %d items plus abstain is %d options, over the cap of %d; narrow in two stages (section, then span): %s",
		e.Items, e.Options, e.Max, narrowingURL)
}

// Unwrap returns ErrTooManyItems.
func (e *TooManyItemsError) Unwrap() error { return ErrTooManyItems }

// KeyError reports a key returned by a KeyFunc that cannot be sent.
type KeyError struct {
	Index  int    // index of the item in the slice given to New
	Key    string // the key as returned
	Reason string // "empty" or "invalid UTF-8"
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("pick: item %d: invalid key %q: %s", e.Index, e.Key, e.Reason)
}

// Unwrap returns ErrInvalidKey.
func (e *KeyError) Unwrap() error { return ErrInvalidKey }

// UnmappedChoiceError reports a choice that is neither a candidate key nor
// the abstain key. A validated answer never produces one; an unvalidated
// answer or a question built elsewhere can.
type UnmappedChoiceError struct{ Choice string }

func (e *UnmappedChoiceError) Error() string {
	return fmt.Sprintf("pick: choice %q does not map to an item", e.Choice)
}

// Unwrap returns ErrUnmappedChoice.
func (e *UnmappedChoiceError) Unwrap() error { return ErrUnmappedChoice }
