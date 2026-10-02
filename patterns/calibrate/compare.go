package calibrate

import (
	"fmt"
	"math"
)

// Metric names one held-out statistic to compare across saved versions.
type Metric string

const (
	MetricErrorRate     Metric = "error_rate"
	MetricCoverage      Metric = "coverage"
	MetricRawBrier      Metric = "raw_brier"
	MetricAdjustedBrier Metric = "adjusted_brier"
	MetricRawECE        Metric = "raw_ece"
	MetricAdjustedECE   Metric = "adjusted_ece"
)

// Direction states which change counts as degradation.
type Direction string

const (
	HigherIsWorse Direction = "higher_is_worse"
	LowerIsWorse  Direction = "lower_is_worse"
)

// Tolerance declares a metric, its adverse direction, and an allowed delta.
type Tolerance struct {
	Metric    Metric    `json:"metric"`
	Direction Direction `json:"direction"`
	MaxDelta  float64   `json:"max_delta"`
}

// Comparison reports the frozen baseline and a candidate model's result.
// Delta is CandidateValue minus BaselineValue. Deterioration is positive
// only in the direction named by Tolerance.
type Comparison struct {
	BaselineModel   string    `json:"baseline_model"`
	CandidateModel  string    `json:"candidate_model"`
	Tolerance       Tolerance `json:"tolerance"`
	BaselineValue   float64   `json:"baseline_value"`
	CandidateValue  float64   `json:"candidate_value"`
	Delta           float64   `json:"delta"`
	Deterioration   float64   `json:"deterioration"`
	Exceeded        bool      `json:"exceeded"`
	CandidateReport Metrics   `json:"candidate_report"`
}

func metricValue(m Metrics, metric Metric) (float64, error) {
	switch metric {
	case MetricErrorRate:
		if m.ErrorRate == nil {
			return 0, ErrUndefinedMetric
		}
		return *m.ErrorRate, nil
	case MetricCoverage:
		return m.Coverage, nil
	case MetricRawBrier:
		return m.RawBrier, nil
	case MetricAdjustedBrier:
		return m.AdjustedBrier, nil
	case MetricRawECE:
		return m.RawECE, nil
	case MetricAdjustedECE:
		return m.AdjustedECE, nil
	default:
		return 0, fmt.Errorf("%w: unknown metric %q", ErrInvalidConfig, metric)
	}
}

// Compare evaluates a candidate's saved answers on the artifact's exact
// held-out cohort. It never refits the cutoff or temperature. An exceeded
// tolerance returns both a populated Comparison and ErrRegression.
func Compare(a Artifact, candidate Dataset, t Tolerance) (Comparison, error) {
	if t.Direction != HigherIsWorse && t.Direction != LowerIsWorse {
		return Comparison{}, fmt.Errorf("%w: direction is required", ErrInvalidConfig)
	}
	if math.IsNaN(t.MaxDelta) || math.IsInf(t.MaxDelta, 0) || t.MaxDelta < 0 {
		return Comparison{}, fmt.Errorf("%w: max_delta must be finite and nonnegative", ErrInvalidConfig)
	}
	if a.FormatVersion != 1 || a.QuestionKey == "" || a.Model == "" ||
		a.QuestionDigest == "" || a.HeldoutDigest == "" || !validUnit(a.Cutoff) ||
		math.IsNaN(a.Temperature) || math.IsInf(a.Temperature, 0) || a.Temperature <= 0 {
		return Comparison{}, fmt.Errorf("%w: invalid artifact", ErrInvalidConfig)
	}
	c, err := checkDataset(candidate)
	if err != nil {
		return Comparison{}, err
	}
	if candidate.QuestionKey != a.QuestionKey || c.questionDigest != a.QuestionDigest ||
		c.cohortDigest != a.HeldoutDigest {
		return Comparison{}, fmt.Errorf("%w: candidate question or cases differ from baseline", ErrCohortMismatch)
	}
	if a.Baseline.Count != len(c.samples) || a.Baseline.Allowed < 0 ||
		a.Baseline.Allowed > a.Baseline.Count || a.Baseline.Errors < 0 ||
		a.Baseline.Errors > a.Baseline.Allowed {
		return Comparison{}, fmt.Errorf("%w: invalid baseline counts", ErrInvalidConfig)
	}
	config, err := validateConfig(c.kind, a.Config)
	if err != nil {
		return Comparison{}, err
	}
	baselineValue, err := metricValue(a.Baseline, t.Metric)
	if err != nil {
		return Comparison{}, err
	}
	if math.IsNaN(baselineValue) || math.IsInf(baselineValue, 0) {
		return Comparison{}, fmt.Errorf("%w: baseline metric is not finite", ErrInvalidConfig)
	}
	ms, err := measureSamples(c.samples, config)
	if err != nil {
		return Comparison{}, err
	}
	report := evaluate(ms, c.kind, config, a.Cutoff, a.Temperature)
	candidateValue, err := metricValue(report, t.Metric)
	if err != nil {
		return Comparison{}, err
	}
	if math.IsNaN(candidateValue) || math.IsInf(candidateValue, 0) {
		return Comparison{}, fmt.Errorf("%w: candidate metric is not finite", ErrInvalidDataset)
	}
	out := Comparison{
		BaselineModel: a.Model, CandidateModel: candidate.Model,
		Tolerance: t, BaselineValue: baselineValue,
		CandidateValue: candidateValue, Delta: candidateValue - baselineValue,
		CandidateReport: report,
	}
	out.Deterioration = out.Delta
	if t.Direction == LowerIsWorse {
		out.Deterioration = -out.Delta
	}
	out.Exceeded = out.Deterioration > t.MaxDelta
	if out.Exceeded {
		return out, fmt.Errorf("%w: %s changed by %g in %s direction (tolerance %g)",
			ErrRegression, t.Metric, out.Deterioration, t.Direction, t.MaxDelta)
	}
	return out, nil
}
