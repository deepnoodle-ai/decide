package gate

import "errors"

// Sentinel errors. Match them with errors.Is.
var (
	// ErrMissing: the input is missing, failed (the error also wraps
	// Input.Err), or abstained.
	ErrMissing = errors.New("gate: input missing")
	// ErrUndefined: the measure is unknown or not defined for the input's kind.
	ErrUndefined = errors.New("gate: measure undefined")
	// ErrBadValue: a value read is NaN, infinite, empty, or outside
	// [-1e-6, 1+1e-6].
	ErrBadValue = errors.New("gate: bad value")
	// ErrInvalidRule: a built-in rule failed Validate.
	ErrInvalidRule = errors.New("gate: invalid rule")
	// ErrUnknownRule: DecodeRule met a "type" it does not know.
	ErrUnknownRule = errors.New("gate: unknown rule type")
)

// Problem strings used in Reading.Problem.
const (
	problemMissing   = "missing"
	problemFailed    = "failed"
	problemAbstained = "abstained"
	problemUndefined = "undefined"
	problemBadValue  = "bad_value"
)

// readError is the error Input.Measure returns. It carries the problem
// string and the reason text rules put in Decision.Reason.
type readError struct {
	problem  string
	reason   string // e.g. `action failed: ...`, without " -> <outcome>"
	sentinel error
	err      error // Input.Err for failed inputs
}

func (e *readError) Error() string { return "gate: " + e.reason }

func (e *readError) Unwrap() []error {
	if e.err != nil {
		return []error{e.sentinel, e.err}
	}
	return []error{e.sentinel}
}
