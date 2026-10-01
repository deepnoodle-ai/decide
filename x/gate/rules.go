package gate

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
)

// Polarity says which end of a measure is safe. The empty polarity is
// invalid.
type Polarity string

// Polarities.
const (
	HighIsSafe  Polarity = "high_is_safe"
	HighIsRisky Polarity = "high_is_risky"
)

// band maps v to an outcome. With HighIsSafe, v >= allow gives Allow, else
// v >= review gives Review, else Escalate; HighIsRisky mirrors it with <=.
// A value on a bound gets the less severe outcome.
func band(p Polarity, v, allow, review float64) Outcome {
	if p == HighIsRisky {
		switch {
		case v <= allow:
			return Allow
		case v <= review:
			return Review
		}
		return Escalate
	}
	switch {
	case v >= allow:
		return Allow
	case v >= review:
		return Review
	}
	return Escalate
}

// weaker reports whether a is strictly weaker than b under p.
func weaker(p Polarity, a, b float64) bool {
	if p == HighIsRisky {
		return a > b
	}
	return a < b
}

func measureName(m Measure, option string) string {
	if m == MeasureProb {
		return string(m) + "(" + option + ")"
	}
	return string(m)
}

// Bands maps one measure of one input to an outcome through two bounds.
// With HighIsSafe, v >= Allow gives Allow, else v >= Review gives Review,
// else Escalate; HighIsRisky mirrors it with <=. Bounds are inclusive; use
// math.Nextafter for a strict bound. JSON type "bands".
type Bands struct {
	Name      string   `json:"name,omitempty"`
	Input     string   `json:"input"`
	Measure   Measure  `json:"measure"`
	Option    string   `json:"option,omitempty"` // MeasureProb only
	Polarity  Polarity `json:"polarity"`
	Allow     float64  `json:"allow"` // the looser bound
	Review    float64  `json:"review"`
	OnMissing Outcome  `json:"on_missing,omitzero"` // unset: Escalate
}

// AllOf is Bands over many inputs: the weakest reading (the minimum for
// HighIsSafe, the maximum for HighIsRisky) is banded, and each input that
// cannot be read contributes OnMissing. The most severe wins. JSON type
// "all_of".
type AllOf struct {
	Name      string   `json:"name,omitempty"`
	Inputs    []string `json:"inputs"` // at least one
	Measure   Measure  `json:"measure"`
	Option    string   `json:"option,omitempty"` // MeasureProb only
	Polarity  Polarity `json:"polarity"`
	Allow     float64  `json:"allow"`
	Review    float64  `json:"review"`
	OnMissing Outcome  `json:"on_missing,omitzero"`
}

// MinConfidence allows when the minimum of a certainty measure over Inputs
// is at least Floor, and gives Below otherwise. List only the inputs that
// should gate, for example the irreversible ones. JSON type
// "min_confidence".
//
// It equals an AllOf with HighIsSafe, Allow == Review == Floor, and Below ==
// Escalate, but it refuses a raw noul or prob as a "confidence".
type MinConfidence struct {
	Name      string   `json:"name,omitempty"`
	Inputs    []string `json:"inputs"`  // at least one
	Measure   Measure  `json:"measure"` // confidence, top, margin, entropy, or applicability
	Floor     float64  `json:"floor"`
	Below     Outcome  `json:"below,omitzero"` // unset: Escalate
	OnMissing Outcome  `json:"on_missing,omitzero"`
}

// Margin allows when the top probability is at least Top and its lead over
// the runner-up is at least Lead, and gives Below otherwise. JSON type
// "margin".
type Margin struct {
	Name      string  `json:"name,omitempty"`
	Input     string  `json:"input"`
	Top       float64 `json:"top"`  // bound on MeasureTop
	Lead      float64 `json:"lead"` // bound on MeasureMargin
	Below     Outcome `json:"below,omitzero"`
	OnMissing Outcome `json:"on_missing,omitzero"`
}

// Asymmetric gates a Choice per chosen option, so a risky option can need a
// higher bar than a safe one. If Floor > 0 and the measure is below Floor,
// the outcome is BelowFloor. Otherwise the chosen option's OptionBand (from
// Options, else Others, else Escalate) decides: its Always if set, else
// HighIsSafe bands on the measure. A non-Choice input counts as missing.
// JSON type "asymmetric".
type Asymmetric struct {
	Name       string                `json:"name,omitempty"`
	Input      string                `json:"input"`
	Measure    Measure               `json:"measure"` // a certainty measure
	Floor      float64               `json:"floor"`   // common floor; 0 means none
	BelowFloor Outcome               `json:"below_floor,omitzero"`
	Options    map[string]OptionBand `json:"options"`          // keyed by option key
	Others     *OptionBand           `json:"others,omitempty"` // unlisted options; nil means Escalate
	OnMissing  Outcome               `json:"on_missing,omitzero"`
}

// OptionBand is the rule for one option of an Asymmetric.
type OptionBand struct {
	Allow  float64 `json:"allow"`
	Review float64 `json:"review"`
	Always Outcome `json:"always,omitzero"` // when set, bounds are ignored
}

// Compose evaluates every rule and returns the most severe decision; ties
// go to the earliest rule. Readings from every rule are concatenated. A
// custom rule's zero or invalid outcome counts as Escalate. JSON type
// "compose".
type Compose struct {
	Name  string `json:"name,omitempty"`
	Rules []Rule `json:"rules"` // at least one
}

func (r Bands) ruleType() string         { return "bands" }
func (r AllOf) ruleType() string         { return "all_of" }
func (r MinConfidence) ruleType() string { return "min_confidence" }
func (r Margin) ruleType() string        { return "margin" }
func (r Asymmetric) ruleType() string    { return "asymmetric" }
func (r Compose) ruleType() string       { return "compose" }

func (r Bands) ruleName() string         { return r.Name }
func (r AllOf) ruleName() string         { return r.Name }
func (r MinConfidence) ruleName() string { return r.Name }
func (r Margin) ruleName() string        { return r.Name }
func (r Asymmetric) ruleName() string    { return r.Name }
func (r Compose) ruleName() string       { return r.Name }

// Evaluate implements Rule.
func (r Bands) Evaluate(in Inputs) Decision { return evaluate(r, in) }

// Evaluate implements Rule.
func (r AllOf) Evaluate(in Inputs) Decision { return evaluate(r, in) }

// Evaluate implements Rule.
func (r MinConfidence) Evaluate(in Inputs) Decision { return evaluate(r, in) }

// Evaluate implements Rule.
func (r Margin) Evaluate(in Inputs) Decision { return evaluate(r, in) }

// Evaluate implements Rule.
func (r Asymmetric) Evaluate(in Inputs) Decision { return evaluate(r, in) }

// Evaluate implements Rule.
func (r Compose) Evaluate(in Inputs) Decision { return evaluate(r, in) }

func bandsReason(input string, m Measure, option string, v float64, o Outcome, allow, review float64, p Polarity) string {
	return fmt.Sprintf("%s %s=%s -> %s (allow %s, review %s, %s)",
		input, measureName(m, option), num(v), o, num(allow), num(review), p)
}

func (r Bands) eval(in Inputs, path string) Decision {
	rd, re := read(in, path, r.Input, r.Measure, r.Option)
	var d Decision
	if re != nil {
		d = missingDecision(path, r.Input, re, r.OnMissing)
	} else {
		o := band(r.Polarity, rd.Value, r.Allow, r.Review)
		d = Decision{Outcome: o, Rule: path, Weakest: r.Input, Value: rd.Value,
			Reason: bandsReason(r.Input, r.Measure, r.Option, rd.Value, o, r.Allow, r.Review, r.Polarity)}
	}
	d.Readings = []Reading{rd}
	return d
}

// multi reads m from each of names, finds the weakest clean reading (ties
// go to the earliest), lets decide turn it into a decision, and then lets
// each bad reading's OnMissing override it when at least as severe. On
// equal severity the earliest missing input is reported.
func multi(in Inputs, path string, names []string, m Measure, option string, p Polarity,
	onMissing Outcome, decide func(name string, v float64) Decision) Decision {
	readings := make([]Reading, 0, len(names))
	var bad []*readError
	var badNames []string
	weakest := -1
	for i, name := range names {
		rd, re := read(in, path, name, m, option)
		readings = append(readings, rd)
		if re != nil {
			bad = append(bad, re)
			badNames = append(badNames, name)
			continue
		}
		if weakest < 0 || weaker(p, rd.Value, readings[weakest].Value) {
			weakest = i
		}
	}
	var d Decision
	have := false
	if weakest >= 0 {
		d, have = decide(names[weakest], readings[weakest].Value), true
	}
	for i, re := range bad {
		md := missingDecision(path, badNames[i], re, onMissing)
		if !have || md.Outcome > d.Outcome || (md.Outcome == d.Outcome && !d.Missing) {
			d, have = md, true
		}
	}
	d.Readings = readings
	return d
}

func (r AllOf) eval(in Inputs, path string) Decision {
	return multi(in, path, r.Inputs, r.Measure, r.Option, r.Polarity, r.OnMissing,
		func(name string, v float64) Decision {
			o := band(r.Polarity, v, r.Allow, r.Review)
			return Decision{Outcome: o, Rule: path, Weakest: name, Value: v,
				Reason: bandsReason(name, r.Measure, r.Option, v, o, r.Allow, r.Review, r.Polarity)}
		})
}

func (r MinConfidence) eval(in Inputs, path string) Decision {
	return multi(in, path, r.Inputs, r.Measure, "", HighIsSafe, r.OnMissing,
		func(name string, v float64) Decision {
			d := Decision{Outcome: Allow, Rule: path, Weakest: name, Value: v}
			if v >= r.Floor {
				d.Reason = fmt.Sprintf("min %s=%s at or above floor %s", r.Measure, num(v), num(r.Floor))
				return d
			}
			d.Outcome = r.Below.orEscalate()
			d.Reason = fmt.Sprintf("%s %s=%s below floor %s -> %s", name, r.Measure, num(v), num(r.Floor), d.Outcome)
			return d
		})
}

func (r Margin) eval(in Inputs, path string) Decision {
	top, reTop := read(in, path, r.Input, MeasureTop, "")
	lead, reLead := read(in, path, r.Input, MeasureMargin, "")
	readings := []Reading{top, lead}
	var d Decision
	switch {
	case reTop != nil:
		d = missingDecision(path, r.Input, reTop, r.OnMissing)
	case reLead != nil:
		d = missingDecision(path, r.Input, reLead, r.OnMissing)
	default:
		o := Allow
		d.Value = lead.Value
		if top.Value < r.Top {
			o, d.Value = r.Below.orEscalate(), top.Value
		} else if lead.Value < r.Lead {
			o = r.Below.orEscalate()
		}
		d.Outcome, d.Rule, d.Weakest = o, path, r.Input
		d.Reason = fmt.Sprintf("%s top=%s (need %s) margin=%s (need %s) -> %s",
			r.Input, num(top.Value), num(r.Top), num(lead.Value), num(r.Lead), o)
	}
	d.Readings = readings
	return d
}

func (r Asymmetric) eval(in Inputs, path string) Decision {
	rd, re := read(in, path, r.Input, r.Measure, "")
	x := in.Get(r.Input)
	if re == nil && x.Kind != KindChoice {
		re = x.undefined(r.Measure).(*readError)
		rd.Value, rd.Problem = 0, re.problem
	}
	if re != nil {
		d := missingDecision(path, r.Input, re, r.OnMissing)
		d.Readings = []Reading{rd}
		return d
	}
	v := rd.Value
	var o Outcome
	var why string
	switch ob, listed := r.Options[x.Choice]; {
	case r.Floor > 0 && v < r.Floor:
		o, why = r.BelowFloor.orEscalate(), "below floor "+num(r.Floor)
	case !listed && r.Others == nil:
		o, why = Escalate, "unlisted option"
	default:
		if !listed {
			ob = *r.Others
		}
		switch {
		case ob.Always != 0:
			o, why = ob.Always.orEscalate(), "always"
		case v >= ob.Allow:
			o, why = Allow, "allow "+num(ob.Allow)
		case v >= ob.Review:
			o, why = Review, "review "+num(ob.Review)
		default:
			o, why = Escalate, "below review "+num(ob.Review)
		}
	}
	return Decision{Outcome: o, Rule: path, Weakest: r.Input, Value: v, Readings: []Reading{rd},
		Reason: fmt.Sprintf("%s chose %s, %s=%s -> %s (%s)", r.Input, x.Choice, r.Measure, num(v), o, why)}
}

func (r Compose) eval(in Inputs, path string) Decision {
	var best Decision
	var readings []Reading
	for i, child := range r.Rules {
		var d Decision
		if b, ok := child.(builtin); ok {
			l := b.ruleName()
			if l == "" {
				l = b.ruleType() + "[" + strconv.Itoa(i) + "]"
			}
			d = b.eval(in, path+"/"+l)
		} else {
			d = child.Evaluate(in)
			p := path + "/custom[" + strconv.Itoa(i) + "]"
			for j := range d.Readings {
				d.Readings[j].Rule = joinPath(p, d.Readings[j].Rule)
			}
			d.Rule = joinPath(p, d.Rule)
			d.Outcome = d.Outcome.orEscalate()
		}
		readings = append(readings, d.Readings...)
		if i == 0 || d.Outcome > best.Outcome {
			best = d
		}
	}
	best.Readings = readings
	return best
}

func joinPath(prefix, rest string) string {
	if rest == "" {
		return prefix
	}
	return prefix + "/" + rest
}

// Validation.

func invalid(b builtin, format string, args ...any) error {
	return fmt.Errorf("%w: %s %q: %s", ErrInvalidRule, b.ruleType(), b.ruleName(), fmt.Sprintf(format, args...))
}

func badBound(f float64) bool { return math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1 }

func checkBounds(b builtin, names []string, vals ...float64) error {
	for i, v := range vals {
		if badBound(v) {
			return invalid(b, "%s is %v, not in [0,1]", names[i], v)
		}
	}
	return nil
}

func checkOutcomes(b builtin, names []string, os ...Outcome) error {
	for i, o := range os {
		if o != 0 && !o.valid() {
			return invalid(b, "%s is %s", names[i], o)
		}
	}
	return nil
}

func checkInputs(b builtin, inputs []string) error {
	if len(inputs) == 0 {
		return invalid(b, "no inputs")
	}
	seen := make(map[string]bool, len(inputs))
	for _, n := range inputs {
		if n == "" {
			return invalid(b, "empty input name")
		}
		if seen[n] {
			return invalid(b, "input %q repeated", n)
		}
		seen[n] = true
	}
	return nil
}

func checkMeasure(b builtin, m Measure, option string, certaintyOnly bool) error {
	switch {
	case m == "":
		return invalid(b, "measure is required")
	case !m.known():
		return invalid(b, "unknown measure %q", m)
	case certaintyOnly && !m.certainty():
		return invalid(b, "measure %q is not a certainty measure", m)
	case m == MeasureProb && option == "":
		return invalid(b, "measure prob needs an option")
	case m != MeasureProb && option != "":
		return invalid(b, "option is set but measure is %q", m)
	}
	return nil
}

func checkPolarity(b builtin, p Polarity, allow, review float64) error {
	switch p {
	case HighIsSafe:
		if allow < review {
			return invalid(b, "high_is_safe needs allow >= review, got %v < %v", allow, review)
		}
	case HighIsRisky:
		if allow > review {
			return invalid(b, "high_is_risky needs allow <= review, got %v > %v", allow, review)
		}
	case "":
		return invalid(b, "polarity is required")
	default:
		return invalid(b, "unknown polarity %q", p)
	}
	return nil
}

func firstErr(errs ...func() error) error {
	for _, f := range errs {
		if err := f(); err != nil {
			return err
		}
	}
	return nil
}

// Validate reports whether r is well formed. Errors wrap ErrInvalidRule.
func (r Bands) Validate() error {
	return firstErr(
		func() error { return checkInputs(r, []string{r.Input}) },
		func() error { return checkMeasure(r, r.Measure, r.Option, false) },
		func() error { return checkBounds(r, []string{"allow", "review"}, r.Allow, r.Review) },
		func() error { return checkPolarity(r, r.Polarity, r.Allow, r.Review) },
		func() error { return checkOutcomes(r, []string{"on_missing"}, r.OnMissing) },
	)
}

// Validate reports whether r is well formed. Errors wrap ErrInvalidRule.
func (r AllOf) Validate() error {
	return firstErr(
		func() error { return checkInputs(r, r.Inputs) },
		func() error { return checkMeasure(r, r.Measure, r.Option, false) },
		func() error { return checkBounds(r, []string{"allow", "review"}, r.Allow, r.Review) },
		func() error { return checkPolarity(r, r.Polarity, r.Allow, r.Review) },
		func() error { return checkOutcomes(r, []string{"on_missing"}, r.OnMissing) },
	)
}

// Validate reports whether r is well formed. Errors wrap ErrInvalidRule.
func (r MinConfidence) Validate() error {
	return firstErr(
		func() error { return checkInputs(r, r.Inputs) },
		func() error { return checkMeasure(r, r.Measure, "", true) },
		func() error { return checkBounds(r, []string{"floor"}, r.Floor) },
		func() error { return checkOutcomes(r, []string{"below", "on_missing"}, r.Below, r.OnMissing) },
	)
}

// Validate reports whether r is well formed. Errors wrap ErrInvalidRule.
func (r Margin) Validate() error {
	return firstErr(
		func() error { return checkInputs(r, []string{r.Input}) },
		func() error { return checkBounds(r, []string{"top", "lead"}, r.Top, r.Lead) },
		func() error { return checkOutcomes(r, []string{"below", "on_missing"}, r.Below, r.OnMissing) },
	)
}

// Validate reports whether r is well formed. Errors wrap ErrInvalidRule.
func (r Asymmetric) Validate() error {
	err := firstErr(
		func() error { return checkInputs(r, []string{r.Input}) },
		func() error { return checkMeasure(r, r.Measure, "", true) },
		func() error { return checkBounds(r, []string{"floor"}, r.Floor) },
		func() error {
			return checkOutcomes(r, []string{"below_floor", "on_missing"}, r.BelowFloor, r.OnMissing)
		},
	)
	if err != nil {
		return err
	}
	check := func(what string, ob OptionBand) error {
		return firstErr(
			func() error { return checkBounds(r, []string{what + ".allow", what + ".review"}, ob.Allow, ob.Review) },
			func() error {
				if ob.Allow < ob.Review {
					return invalid(r, "%s needs allow >= review, got %v < %v", what, ob.Allow, ob.Review)
				}
				return nil
			},
			func() error { return checkOutcomes(r, []string{what + ".always"}, ob.Always) },
		)
	}
	for k, ob := range r.Options {
		if k == "" {
			return invalid(r, "empty option key")
		}
		if err := check(fmt.Sprintf("options[%q]", k), ob); err != nil {
			return err
		}
	}
	if r.Others != nil {
		return check("others", *r.Others)
	}
	return nil
}

// Validate reports whether r is well formed: it has at least one rule, no
// nil rule, and every rule that implements Validator is valid. Errors wrap
// ErrInvalidRule.
func (r Compose) Validate() error {
	if len(r.Rules) == 0 {
		return invalid(r, "no rules")
	}
	for i, child := range r.Rules {
		if child == nil {
			return invalid(r, "rule %d is nil", i)
		}
		if v := reflect.ValueOf(child); v.Kind() == reflect.Pointer && v.IsNil() {
			return invalid(r, "rule %d is a nil %T", i, child)
		}
		v, ok := child.(Validator)
		if !ok {
			continue
		}
		if err := v.Validate(); err != nil {
			if errors.Is(err, ErrInvalidRule) {
				return fmt.Errorf("%s[%d]: %w", r.ruleType(), i, err)
			}
			return invalid(r, "rule %d: %v", i, err)
		}
	}
	return nil
}
