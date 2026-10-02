package gate

// Decision is the result of evaluating a rule.
type Decision struct {
	Outcome  Outcome   `json:"outcome"`
	Rule     string    `json:"rule"`              // path to the rule that fired, e.g. "file_op/asymmetric[0]"
	Reason   string    `json:"reason"`            // short human-readable reason
	Weakest  string    `json:"weakest,omitempty"` // input name that decided it; "" if none
	Value    float64   `json:"value"`             // Weakest's measured value; 0 when Missing
	Missing  bool      `json:"missing"`           // a missing, failed, abstained, or undefined input decided it
	Readings []Reading `json:"readings"`          // every reading taken, in evaluation order, for audit logs
}

// Reading is one measure read from one input. Value is 0, never NaN, when
// Problem is set, so a Decision always encodes as JSON.
type Reading struct {
	Rule    string  `json:"rule"`
	Input   string  `json:"input"`
	Measure Measure `json:"measure"`
	Option  string  `json:"option,omitempty"`
	Value   float64 `json:"value"`
	Problem string  `json:"problem,omitempty"` // "missing", "failed", "abstained", "undefined", "bad_value"
}

// Rule is the extension point. Evaluate must be pure and must not panic.
type Rule interface {
	Evaluate(in Inputs) Decision
}

// Validator is implemented by every built-in rule. Evaluate on an invalid
// built-in returns Escalate with Reason "invalid rule: <err>", never a
// panic.
type Validator interface {
	Validate() error
}

// builtin is implemented by every built-in rule.
type builtin interface {
	Rule
	Validator
	ruleType() string
	ruleName() string
	eval(in Inputs, path string) Decision
}

func label(b builtin) string {
	if n := b.ruleName(); n != "" {
		return n
	}
	return b.ruleType()
}

// evaluate is the body of every built-in's Evaluate.
func evaluate(b builtin, in Inputs) Decision {
	path := label(b)
	if err := b.Validate(); err != nil {
		return Decision{Outcome: Escalate, Rule: path, Reason: "invalid rule: " + err.Error()}
	}
	return b.eval(in, path)
}

// read takes one reading.
func read(in Inputs, path, name string, m Measure, option string) (Reading, *readError) {
	r := Reading{Rule: path, Input: name, Measure: m}
	if m == MeasureProb {
		r.Option = option
	}
	v, err := in.Get(name).Measure(m, option)
	if err != nil {
		re := err.(*readError)
		r.Problem = re.problem
		return r, re
	}
	r.Value = v
	return r, nil
}

func missingDecision(path, name string, re *readError, onMissing Outcome) Decision {
	o := onMissing.orEscalate()
	return Decision{Outcome: o, Rule: path, Reason: re.reason + " -> " + o.String(),
		Weakest: name, Missing: true}
}
