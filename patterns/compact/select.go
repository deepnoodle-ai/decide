package compact

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
)

var (
	// ErrInvalidInput reports an invalid argument, rule, or score.
	ErrInvalidInput = errors.New("compact: invalid input")
	// ErrOverBudget reports pinned segments whose sizes exceed the budget.
	ErrOverBudget = errors.New("compact: pinned segments exceed the budget")
	// ErrTooLarge reports a state or segment too large for the request
	// limits.
	ErrTooLarge = errors.New("compact: too large for the request limits")
)

// Segment is one piece of the context. Text is kept verbatim and is sent to
// the model inside its question. Short is an optional stand-in the caller
// wrote in code, such as a truncated tool result; "" means none. Sizes are in
// the caller's unit, such as tokens. A pinned segment is always kept whole
// and never asked about.
type Segment struct {
	Text      string
	Short     string
	Size      int
	ShortSize int
	Pinned    bool
}

// Scores are the judgments for one segment. Needed is P(the segment is still
// needed). Verbatim is P(its exact full text is needed), set only when
// HasVerbatim. Err means the segment could not be scored.
type Scores struct {
	Needed      float64
	Verbatim    float64
	HasVerbatim bool
	Err         error
}

// Rule controls selection. Budget is the most total size to keep. A scored
// segment with Needed below Floor is dropped. A segment with a short form
// uses it when Verbatim is below ShortBelow. The package ships no values;
// zero Floor and ShortBelow disable those checks.
type Rule struct {
	Budget     int
	Floor      float64
	ShortBelow float64
}

// Action is what happens to a segment.
type Action string

const (
	Keep  Action = "keep"  // kept whole
	Short Action = "short" // replaced by its short form
	Drop  Action = "drop"
)

// Cause says why a segment got its action.
type Cause string

const (
	CausePinned   Cause = "pinned"   // pinned, kept whole
	CauseRanked   Cause = "ranked"   // kept by score order within the budget
	CauseUnscored Cause = "unscored" // could not be scored, kept ahead of scored segments
	CauseFloor    Cause = "floor"    // Needed below the rule's floor
	CauseBudget   Cause = "budget"   // no form fit the remaining budget
)

// Decision is the outcome for the segment at Index.
type Decision struct {
	Index  int
	Action Action
	Cause  Cause
	Scores Scores
}

// Select decides every segment's action under rule, in input order. It is
// pure. Pinned segments are kept first, then unscored ones in input order,
// then scored ones by descending Needed, more recent first on ties. A segment
// whose preferred form does not fit tries its short form, then is dropped;
// selection continues with the next. When pinned segments alone exceed the
// budget, the decisions are returned with ErrOverBudget.
func Select(segs []Segment, scores []Scores, rule Rule) ([]Decision, error) {
	if len(segs) != len(scores) {
		return nil, fmt.Errorf("%w: %d segments but %d scores", ErrInvalidInput, len(segs), len(scores))
	}
	if err := checkRule(rule); err != nil {
		return nil, err
	}
	if err := checkSegments(segs); err != nil {
		return nil, err
	}
	for i, s := range scores {
		if segs[i].Pinned || s.Err != nil {
			continue
		}
		if badProb(s.Needed) || (s.HasVerbatim && badProb(s.Verbatim)) {
			return nil, fmt.Errorf("%w: segment %d has a score outside [0,1]", ErrInvalidInput, i)
		}
	}

	out := make([]Decision, len(segs))
	left := rule.Budget
	var unscored, scored []int
	for i := range segs {
		out[i] = Decision{Index: i, Scores: scores[i]}
		switch {
		case segs[i].Pinned:
			out[i].Action, out[i].Cause = Keep, CausePinned
			left -= segs[i].Size
		case scores[i].Err != nil:
			unscored = append(unscored, i)
		case scores[i].Needed < rule.Floor:
			out[i].Action, out[i].Cause = Drop, CauseFloor
		default:
			scored = append(scored, i)
		}
	}
	if left < 0 {
		for _, i := range slices.Concat(unscored, scored) {
			out[i].Action, out[i].Cause = Drop, CauseBudget
		}
		return out, ErrOverBudget
	}

	take := func(i int, preferShort bool, cause Cause) {
		s := segs[i]
		hasShort := s.Short != ""
		switch {
		case preferShort && s.ShortSize <= left:
			out[i].Action, out[i].Cause = Short, cause
			left -= s.ShortSize
		case !preferShort && s.Size <= left:
			out[i].Action, out[i].Cause = Keep, cause
			left -= s.Size
		case hasShort && s.ShortSize <= left:
			out[i].Action, out[i].Cause = Short, cause
			left -= s.ShortSize
		default:
			out[i].Action, out[i].Cause = Drop, CauseBudget
		}
	}
	for _, i := range unscored {
		take(i, false, CauseUnscored)
	}
	slices.SortStableFunc(scored, func(a, b int) int {
		return cmp.Or(cmp.Compare(scores[b].Needed, scores[a].Needed), cmp.Compare(b, a))
	})
	for _, i := range scored {
		s := scores[i]
		take(i, segs[i].Short != "" && s.HasVerbatim && s.Verbatim < rule.ShortBelow, CauseRanked)
	}
	return out, nil
}

func checkRule(r Rule) error {
	if r.Budget <= 0 || badProb(r.Floor) || badProb(r.ShortBelow) {
		return fmt.Errorf("%w: rule needs Budget > 0 and Floor, ShortBelow in [0,1]", ErrInvalidInput)
	}
	return nil
}

func checkSegments(segs []Segment) error {
	for i, s := range segs {
		if s.Size < 0 || s.ShortSize < 0 {
			return fmt.Errorf("%w: segment %d has a negative size", ErrInvalidInput, i)
		}
	}
	return nil
}

func badProb(p float64) bool { return math.IsNaN(p) || p < 0 || p > 1 }
