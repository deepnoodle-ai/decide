package gate

import (
	"fmt"
	"strconv"
)

// Outcome is the result of a rule, ordered by severity: Allow < Review <
// Escalate. The zero value is not a valid outcome; every Outcome field in a
// rule treats zero as unset, and unset means Escalate.
type Outcome uint8

// The three outcomes, in order of severity.
const (
	Allow    Outcome = 1
	Review   Outcome = 2
	Escalate Outcome = 3
)

// String returns "allow", "review", or "escalate", else "Outcome(n)".
func (o Outcome) String() string {
	switch o {
	case Allow:
		return "allow"
	case Review:
		return "review"
	case Escalate:
		return "escalate"
	}
	return "Outcome(" + strconv.Itoa(int(o)) + ")"
}

func (o Outcome) valid() bool { return o >= Allow && o <= Escalate }

// MarshalText returns the name of o. It fails for invalid values, including
// zero.
func (o Outcome) MarshalText() ([]byte, error) {
	if !o.valid() {
		return nil, fmt.Errorf("gate: invalid outcome %d", uint8(o))
	}
	return []byte(o.String()), nil
}

// UnmarshalText accepts exactly "allow", "review", or "escalate".
func (o *Outcome) UnmarshalText(b []byte) error {
	switch string(b) {
	case "allow":
		*o = Allow
	case "review":
		*o = Review
	case "escalate":
		*o = Escalate
	default:
		return fmt.Errorf("gate: invalid outcome %q", b)
	}
	return nil
}

// orEscalate maps an unset or invalid outcome to Escalate.
func (o Outcome) orEscalate() Outcome {
	if !o.valid() {
		return Escalate
	}
	return o
}

// Worse returns the more severe of a and b. Zero (and any invalid value)
// counts as Escalate, so the result is always a valid outcome.
func Worse(a, b Outcome) Outcome { return max(a.orEscalate(), b.orEscalate()) }
