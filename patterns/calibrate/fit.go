package calibrate

import (
	"fmt"
	"math"
	"slices"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// Config names the raw gate measure and the empirical fit-set objective.
// Temperatures is an optional discrete search grid; identity is always tried.
type Config struct {
	Measure      gate.Measure  `json:"measure"`
	Polarity     gate.Polarity `json:"polarity"`
	NoulTarget   string        `json:"noul_target,omitempty"`
	MaxError     float64       `json:"max_error"`
	MinAllowed   int           `json:"min_allowed"`
	Temperatures []float64     `json:"temperatures,omitempty"`
	ECEBins      int           `json:"ece_bins"`
}

// Candidate is one observed inclusive cutoff and its fit-set result.
type Candidate struct {
	Cutoff    float64 `json:"cutoff"`
	Allowed   int     `json:"allowed"`
	Errors    int     `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
}

// Artifact freezes a fit and its held-out baseline for later comparison.
// It contains no state, question text, or answer bytes.
type Artifact struct {
	FormatVersion  int         `json:"format_version"`
	QuestionKey    string      `json:"question_key"`
	QuestionDigest string      `json:"question_digest"`
	FitDigest      string      `json:"fit_digest"`
	HeldoutDigest  string      `json:"heldout_digest"`
	Model          string      `json:"model"`
	Config         Config      `json:"config"`
	Cutoff         float64     `json:"cutoff"`
	Temperature    float64     `json:"temperature"`
	Candidates     []Candidate `json:"candidates"`
	Baseline       Metrics     `json:"baseline"`
}

type measured struct {
	sample sample
	value  float64
}

func validUnit(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}

func validateConfig(kind gate.Kind, c Config) (Config, error) {
	if !validUnit(c.MaxError) || c.MinAllowed < 1 || c.ECEBins < 1 {
		return c, fmt.Errorf("%w: max_error must be in [0,1]; min_allowed and ece_bins must be positive", ErrInvalidConfig)
	}
	for _, temp := range c.Temperatures {
		if math.IsNaN(temp) || math.IsInf(temp, 0) || temp <= 0 {
			return c, fmt.Errorf("%w: temperatures must be positive and finite", ErrInvalidConfig)
		}
	}
	switch kind {
	case gate.KindNoul:
		if c.Measure != gate.MeasureNoul || (c.NoulTarget != "true" && c.NoulTarget != "false") {
			return c, fmt.Errorf("%w: Noul requires measure noul and a true/false target", ErrInvalidConfig)
		}
		want := gate.HighIsSafe
		if c.NoulTarget == "false" {
			want = gate.HighIsRisky
		}
		if c.Polarity != "" && c.Polarity != want {
			return c, fmt.Errorf("%w: Noul target needs polarity %s", ErrInvalidConfig, want)
		}
		c.Polarity = want
	case gate.KindChoice, gate.KindScore:
		if c.NoulTarget != "" {
			return c, fmt.Errorf("%w: only Noul uses noul_target", ErrInvalidConfig)
		}
		switch c.Measure {
		case gate.MeasureConfidence, gate.MeasureTop, gate.MeasureMargin, gate.MeasureEntropy:
		default:
			return c, fmt.Errorf("%w: Choice/Score needs a certainty measure", ErrInvalidConfig)
		}
		if c.Polarity != gate.HighIsSafe && c.Polarity != gate.HighIsRisky {
			return c, fmt.Errorf("%w: Choice/Score needs a polarity", ErrInvalidConfig)
		}
	}
	return c, nil
}

func measureSamples(cs []sample, c Config) ([]measured, error) {
	out := make([]measured, 0, len(cs))
	for _, s := range cs {
		v, err := s.input.Measure(c.Measure, "")
		if err != nil {
			return nil, fmt.Errorf("%w: case %q: gate measure %q: %v", ErrInvalidDataset, s.id, c.Measure, err)
		}
		if !validUnit(v) {
			return nil, fmt.Errorf("%w: case %q: gate measure %q outside [0,1]", ErrInvalidDataset, s.id, c.Measure)
		}
		out = append(out, measured{sample: s, value: v})
	}
	return out, nil
}

func allowed(value, cutoff float64, p gate.Polarity) bool {
	if p == gate.HighIsRisky {
		return value <= cutoff
	}
	return value >= cutoff
}

func wrong(s sample, c Config, kind gate.Kind) bool {
	if kind == gate.KindNoul {
		return s.label != c.NoulTarget
	}
	return s.label != s.predicted
}

// Fit selects a cutoff on fit cases, then evaluates it on held-out cases.
// The held-out set never changes the selected cutoff or temperature.
func Fit(fit, heldout Dataset, config Config) (Artifact, error) {
	f, err := checkDataset(fit)
	if err != nil {
		return Artifact{}, err
	}
	h, err := checkDataset(heldout)
	if err != nil {
		return Artifact{}, err
	}
	if f.questionDigest != h.questionDigest || f.model != h.model {
		return Artifact{}, fmt.Errorf("%w: fit and held-out question or model differ", ErrCohortMismatch)
	}
	ids := make(map[string]bool, len(f.samples))
	for _, s := range f.samples {
		ids[s.id] = true
	}
	for _, s := range h.samples {
		if ids[s.id] {
			return Artifact{}, fmt.Errorf("%w: case %q appears in fit and held-out", ErrCohortMismatch, s.id)
		}
	}
	config, err = validateConfig(f.kind, config)
	if err != nil {
		return Artifact{}, err
	}
	fs, err := measureSamples(f.samples, config)
	if err != nil {
		return Artifact{}, err
	}
	hs, err := measureSamples(h.samples, config)
	if err != nil {
		return Artifact{}, err
	}
	values := make([]float64, 0, len(fs))
	for _, s := range fs {
		values = append(values, s.value)
	}
	slices.Sort(values)
	values = slices.Compact(values)
	candidates := make([]Candidate, 0, len(values))
	var best Candidate
	found := false
	for _, cutoff := range values {
		candidate := Candidate{Cutoff: cutoff}
		for _, s := range fs {
			if !allowed(s.value, cutoff, config.Polarity) {
				continue
			}
			candidate.Allowed++
			if wrong(s.sample, config, f.kind) {
				candidate.Errors++
			}
		}
		candidate.ErrorRate = float64(candidate.Errors) / float64(candidate.Allowed)
		candidates = append(candidates, candidate)
		if candidate.Allowed < config.MinAllowed || candidate.ErrorRate > config.MaxError {
			continue
		}
		if !found || candidate.Allowed > best.Allowed ||
			(candidate.Allowed == best.Allowed && stricter(candidate.Cutoff, best.Cutoff, config.Polarity)) {
			best, found = candidate, true
		}
	}
	if !found {
		return Artifact{
			FormatVersion: 1, QuestionKey: fit.QuestionKey,
			QuestionDigest: f.questionDigest, FitDigest: f.cohortDigest,
			HeldoutDigest: h.cohortDigest, Model: f.model,
			Config: config, Candidates: candidates,
		}, ErrNoFit
	}
	temperature := fitTemperature(f.samples, config.Temperatures)
	metrics := evaluate(hs, h.kind, config, best.Cutoff, temperature)
	return Artifact{
		FormatVersion: 1, QuestionKey: fit.QuestionKey,
		QuestionDigest: f.questionDigest, FitDigest: f.cohortDigest,
		HeldoutDigest: h.cohortDigest, Model: f.model,
		Config: config, Cutoff: best.Cutoff,
		Temperature: temperature, Candidates: candidates, Baseline: metrics,
	}, nil
}

func stricter(a, b float64, p gate.Polarity) bool {
	if p == gate.HighIsRisky {
		return a < b
	}
	return a > b
}

// Rule returns the fitted bound as an patterns/gate Bands rule. A caller may
// choose a separate Review bound; by default every non-allow escalates.
func (a Artifact) Rule(input string) (gate.Bands, error) {
	if a.FormatVersion != 1 || a.Temperature <= 0 || a.Model == "" {
		return gate.Bands{}, fmt.Errorf("%w: artifact has no fit", ErrNoFit)
	}
	r := gate.Bands{
		Input: input, Measure: a.Config.Measure, Polarity: a.Config.Polarity,
		Allow: a.Cutoff, Review: a.Cutoff, OnMissing: gate.Escalate,
	}
	if err := r.Validate(); err != nil {
		return gate.Bands{}, err
	}
	return r, nil
}
