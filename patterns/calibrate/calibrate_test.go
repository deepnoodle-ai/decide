package calibrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/deepnoodle-ai/decide"
	"github.com/deepnoodle-ai/decide/decidetest"
	"github.com/deepnoodle-ai/decide/patterns/gate"
)

func raw(tb testing.TB, v any) json.RawMessage {
	tb.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func noulSet(t *testing.T, model string, ps []float64, labels []string) Dataset {
	t.Helper()
	d := Dataset{QuestionKey: "is_urgent", Question: raw(t, decide.Noul("Is this urgent?")), Model: model}
	for i, p := range ps {
		d.Cases = append(d.Cases, Case{
			ID: string(rune('a' + i)), State: raw(t, "case "+string(rune('a'+i))),
			Answer: raw(t, decidetest.NoulAnswer(p)), Label: labels[i],
		})
	}
	return d
}

func config() Config {
	return Config{Measure: gate.MeasureNoul, NoulTarget: "true", MaxError: 0,
		MinAllowed: 1, ECEBins: 5}
}

func TestNoulFitAndFrozenComparison(t *testing.T) {
	fit := noulSet(t, "jev-1.13.0", []float64{0.9, 0.8, 0.7, 0.4},
		[]string{"true", "false", "true", "false"})
	holdout := noulSet(t, "jev-1.13.0", []float64{0.95, 0.85, 0.2},
		[]string{"true", "false", "false"})
	for i := range holdout.Cases {
		holdout.Cases[i].ID = "h" + holdout.Cases[i].ID
	}
	before := raw(t, fit)
	a, err := Fit(fit, holdout, config())
	if err != nil {
		t.Fatal(err)
	}
	if a.Cutoff != 0.9 || a.Temperature != 1 || a.Baseline.Allowed != 1 ||
		a.Baseline.Errors != 0 || a.Baseline.Coverage != 1.0/3 {
		t.Fatalf("fit: %+v", a)
	}
	if !bytes.Equal(raw(t, fit), before) {
		t.Fatal("Fit mutated its input")
	}
	reorderedFit, reorderedHoldout := fit, holdout
	reorderedFit.Cases = append([]Case(nil), fit.Cases...)
	reorderedHoldout.Cases = append([]Case(nil), holdout.Cases...)
	slices.Reverse(reorderedFit.Cases)
	slices.Reverse(reorderedHoldout.Cases)
	again, err := Fit(reorderedFit, reorderedHoldout, config())
	if err != nil || !bytes.Equal(raw(t, a), raw(t, again)) {
		t.Fatalf("reordered fit changed artifact: %v", err)
	}
	rule, err := a.Rule("is_urgent")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Measure != gate.MeasureNoul || rule.Polarity != gate.HighIsSafe ||
		rule.Allow != 0.9 || rule.Review != 0.9 {
		t.Fatalf("rule: %+v", rule)
	}
	if got := rule.Evaluate(gate.NewInputs(gate.Noul("is_urgent", decidetest.NoulAnswer(0.9)))).Outcome; got != gate.Allow {
		t.Fatalf("at boundary: %v", got)
	}
	if got := rule.Evaluate(gate.NewInputs(gate.Noul("is_urgent", decidetest.NoulAnswer(0.89)))).Outcome; got != gate.Escalate {
		t.Fatalf("below boundary: %v", got)
	}
	var roundtrip Artifact
	if err := json.Unmarshal(raw(t, a), &roundtrip); err != nil {
		t.Fatal(err)
	}
	c := holdout
	c.Model = "jev-1.14.0"
	same, err := Compare(roundtrip, c, Tolerance{Metric: MetricErrorRate,
		Direction: HigherIsWorse, MaxDelta: 0})
	if err != nil || same.Exceeded || same.CandidateModel != c.Model {
		t.Fatalf("same: %+v, %v", same, err)
	}
	c.Cases = append([]Case(nil), c.Cases...)
	c.Cases[1].Answer = raw(t, decidetest.NoulAnswer(0.99))
	regression, err := Compare(roundtrip, c, Tolerance{Metric: MetricErrorRate,
		Direction: HigherIsWorse, MaxDelta: 0.1})
	if !errors.Is(err, ErrRegression) || !regression.Exceeded || regression.CandidateValue != 0.5 {
		t.Fatalf("regression: %+v, %v", regression, err)
	}
	_, err = Compare(roundtrip, c, Tolerance{Metric: MetricCoverage,
		Direction: LowerIsWorse, MaxDelta: 0})
	if err != nil {
		t.Fatalf("coverage rose: %v", err)
	}
	c.Cases[0].Answer = raw(t, decidetest.NoulAnswer(0.5))
	c.Cases[1].Answer = raw(t, decidetest.NoulAnswer(0.85))
	shrunk, err := Compare(roundtrip, c, Tolerance{Metric: MetricCoverage,
		Direction: LowerIsWorse, MaxDelta: 0})
	if !errors.Is(err, ErrRegression) || !shrunk.Exceeded || shrunk.CandidateReport.Allowed != 0 {
		t.Fatalf("coverage regression: %+v, %v", shrunk, err)
	}
	c.Cases[1].Label = "true"
	if _, err := Compare(roundtrip, c, Tolerance{Metric: MetricCoverage,
		Direction: LowerIsWorse, MaxDelta: 0}); !errors.Is(err, ErrCohortMismatch) {
		t.Fatalf("changed label: %v", err)
	}
}

func TestNoulFalseNoFitAndZeroAllowed(t *testing.T) {
	f := noulSet(t, "jev-1.13.0", []float64{0.1, 0.2}, []string{"false", "true"})
	h := noulSet(t, "jev-1.13.0", []float64{0.8, 0.9}, []string{"true", "true"})
	for i := range h.Cases {
		h.Cases[i].ID = "h" + h.Cases[i].ID
	}
	c := config()
	c.NoulTarget = "false"
	a, err := Fit(f, h, c)
	if err != nil {
		t.Fatal(err)
	}
	if a.Config.Polarity != gate.HighIsRisky || a.Cutoff != 0.1 ||
		a.Baseline.ErrorRate != nil || a.Baseline.Allowed != 0 {
		t.Fatalf("fit: %+v", a)
	}
	c.MinAllowed = 3
	no, err := Fit(f, h, c)
	if !errors.Is(err, ErrNoFit) || len(no.Candidates) != 2 {
		t.Fatalf("no fit: %+v, %v", no, err)
	}
	if _, err := no.Rule("is_urgent"); !errors.Is(err, ErrNoFit) {
		t.Fatalf("no-fit rule: %v", err)
	}
}

func TestChoiceAndScorePredictions(t *testing.T) {
	choice := decide.Choice("Which?", decide.Option("a", "A"), decide.Option("b", "B"))
	ch := Dataset{QuestionKey: "which", Question: raw(t, choice), Model: "jev-1.13.0",
		Cases: []Case{{ID: "a", State: raw(t, "first"), Answer: raw(t,
			decidetest.ChoiceAnswer(map[string]float64{"a": 0.9, "b": 0.1})), Label: "a"}}}
	chHold := ch
	chHold.Cases = []Case{{ID: "b", State: raw(t, "second"), Answer: raw(t,
		decidetest.ChoiceAnswer(map[string]float64{"a": 0.7, "b": 0.3})), Label: "b"}}
	cc := Config{Measure: gate.MeasureConfidence, Polarity: gate.HighIsSafe,
		MaxError: 0, MinAllowed: 1, ECEBins: 2}
	a, err := Fit(ch, chHold, cc)
	if err != nil {
		t.Fatal(err)
	}
	if a.Baseline.Errors != 0 || a.Baseline.Allowed != 0 || a.Baseline.ErrorRate != nil {
		t.Fatalf("choice baseline: %+v", a.Baseline)
	}
	levels := []any{"low", "high"}
	score := decide.Score("Severity?", levels...)
	s := Dataset{QuestionKey: "severity", Question: raw(t, score), Model: "jev-1.13.0",
		Cases: []Case{{ID: "a", State: raw(t, "first"), Answer: raw(t,
			decidetest.ScoreAnswer(levels, 0.5, 0.5)), Label: "0"}}}
	sHold := s
	sHold.Cases = []Case{{ID: "b", State: raw(t, "second"), Answer: raw(t,
		decidetest.ScoreAnswer(levels, 0.5, 0.5)), Label: "1"}}
	sc := Config{Measure: gate.MeasureTop, Polarity: gate.HighIsSafe,
		MaxError: 0, MinAllowed: 1, ECEBins: 2}
	a, err = Fit(s, sHold, sc)
	if err != nil {
		t.Fatal(err)
	}
	if a.Baseline.Errors != 1 || a.Baseline.Allowed != 1 || *a.Baseline.ErrorRate != 1 {
		t.Fatalf("score tie prediction: %+v", a.Baseline)
	}
}

func TestTemperatureAndMetrics(t *testing.T) {
	f := noulSet(t, "jev-1.13.0", []float64{0.9, 0.9, 0.9},
		[]string{"false", "false", "true"})
	h := noulSet(t, "jev-1.13.0", []float64{0.8, 0.2}, []string{"false", "true"})
	for i := range h.Cases {
		h.Cases[i].ID = "h" + h.Cases[i].ID
	}
	c := config()
	c.MaxError = 1
	c.Temperatures = []float64{0.5, 4}
	a, err := Fit(f, h, c)
	if err != nil {
		t.Fatal(err)
	}
	if a.Temperature != 4 || !(a.Baseline.AdjustedBrier < a.Baseline.RawBrier) {
		t.Fatalf("temperature: %+v", a)
	}
	if math.Abs(a.Baseline.RawBrier-0.64) > 1e-12 {
		t.Fatalf("raw Brier: %v", a.Baseline.RawBrier)
	}
	if math.Abs(a.Baseline.RawECE-0.8) > 1e-12 {
		t.Fatalf("raw ECE: %v", a.Baseline.RawECE)
	}
}

func TestValidation(t *testing.T) {
	f := noulSet(t, "jev-1.13.0", []float64{0.9}, []string{"true"})
	h := noulSet(t, "jev-1.13.0", []float64{0.1}, []string{"false"})
	h.Cases[0].ID = "h"
	tests := []struct {
		name     string
		edit     func(*Dataset)
		want     error
		caseName string
	}{
		{"duplicate", func(d *Dataset) { d.Cases = append(d.Cases, d.Cases[0]) }, ErrInvalidDataset, "a"},
		{"bad label", func(d *Dataset) { d.Cases[0].Label = "maybe" }, ErrInvalidDataset, "a"},
		{"missing state", func(d *Dataset) { d.Cases[0].State = nil }, ErrInvalidDataset, "a"},
		{"wrong answer", func(d *Dataset) { d.Cases[0].Answer = raw(t, decidetest.ChoiceAnswer(map[string]float64{"a": 1})) }, ErrInvalidDataset, "a"},
		{"bad probability", func(d *Dataset) { d.Cases[0].Answer = []byte(`{"type":"noul","noul":1.1}`) }, ErrInvalidDataset, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := f
			d.Cases = append([]Case(nil), f.Cases...)
			tt.edit(&d)
			_, err := Fit(d, h, config())
			if !errors.Is(err, tt.want) || !strings.Contains(err.Error(), tt.caseName) {
				t.Fatalf("%v", err)
			}
		})
	}
	if _, err := Fit(f, f, config()); !errors.Is(err, ErrCohortMismatch) {
		t.Fatalf("overlap: %v", err)
	}
	a, err := Fit(f, h, config())
	if err != nil {
		t.Fatal(err)
	}
	badQuestion := h
	badQuestion.Question = raw(t, decide.Noul("A changed question?"))
	if _, err := Compare(a, badQuestion, Tolerance{Metric: MetricCoverage,
		Direction: LowerIsWorse, MaxDelta: 0}); !errors.Is(err, ErrCohortMismatch) {
		t.Fatalf("question mismatch: %v", err)
	}
	badState := h
	badState.Cases = append([]Case(nil), h.Cases...)
	badState.Cases[0].State = raw(t, "changed")
	if _, err := Compare(a, badState, Tolerance{Metric: MetricCoverage,
		Direction: LowerIsWorse, MaxDelta: 0}); !errors.Is(err, ErrCohortMismatch) {
		t.Fatalf("state mismatch: %v", err)
	}
}

func TestChoiceAnswerValidation(t *testing.T) {
	q := decide.Choice("Which route?",
		decide.Option("a", "Route A"), decide.Option("b", "Route B"))
	base := Dataset{QuestionKey: "route", Question: raw(t, q), Model: "jev-1.13.0",
		Cases: []Case{{ID: "f", State: raw(t, "first"), Answer: raw(t,
			decidetest.ChoiceAnswer(map[string]float64{"a": 0.8, "b": 0.2})), Label: "a"}}}
	hold := base
	hold.Cases = []Case{{ID: "h", State: raw(t, "second"), Answer: raw(t,
		decidetest.ChoiceAnswer(map[string]float64{"a": 0.7, "b": 0.3})), Label: "a"}}
	c := Config{Measure: gate.MeasureConfidence, Polarity: gate.HighIsSafe,
		MaxError: 0, MinAllowed: 1, ECEBins: 2}
	tests := []struct {
		name   string
		answer json.RawMessage
	}{
		{"missing confidence", []byte(`{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2}}`)},
		{"wrong support", []byte(`{"type":"choice","choice":"a","probabilities":{"a":0.8,"c":0.2},"confidence":0.6}`)},
		{"wrong sum", []byte(`{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.1},"confidence":0.6}`)},
		{"not argmax", []byte(`{"type":"choice","choice":"b","probabilities":{"a":0.8,"b":0.2},"confidence":0.6}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := base
			d.Cases = append([]Case(nil), d.Cases...)
			d.Cases[0].Answer = tt.answer
			_, err := Fit(d, hold, c)
			if !errors.Is(err, ErrInvalidDataset) || !strings.Contains(err.Error(), `case "f"`) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestInvalidConfigAndUncomparableErrorRate(t *testing.T) {
	f := noulSet(t, "jev-1.13.0", []float64{0.1}, []string{"false"})
	h := noulSet(t, "jev-1.13.0", []float64{0.9}, []string{"true"})
	h.Cases[0].ID = "h"
	c := config()
	c.NoulTarget = "false"
	c.Polarity = gate.HighIsSafe
	if _, err := Fit(f, h, c); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong polarity: %v", err)
	}
	c.Polarity = ""
	c.Temperatures = []float64{math.NaN()}
	if _, err := Fit(f, h, c); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NaN temperature: %v", err)
	}
	c.Temperatures = nil
	a, err := Fit(f, h, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compare(a, h, Tolerance{Metric: MetricErrorRate,
		Direction: HigherIsWorse}); !errors.Is(err, ErrUndefinedMetric) {
		t.Fatalf("null baseline error: %v", err)
	}
}

func TestRejectZeroMassManyOptionChoice(t *testing.T) {
	options := make([]decide.ChoiceOption, 190)
	zero := make(map[string]float64, len(options))
	for i := range options {
		key := fmt.Sprintf("o%03d", i)
		options[i] = decide.Option(key)
		zero[key] = 0
	}
	q := decide.Choice("Which option?", options...)
	zeroAnswer := decidetest.ChoiceAnswer(zero)
	if err := q.ValidateAnswer(zeroAnswer); err != nil {
		t.Fatalf("fixture no longer reproduces root tolerance: %v", err)
	}
	valid := make(map[string]float64, len(zero))
	for key := range zero {
		valid[key] = 0
	}
	valid["o000"], valid["o001"] = 0.9, 0.1
	base := Dataset{
		QuestionKey: "which", Question: raw(t, q), Model: "jev-1.13.0",
		Cases: []Case{{
			ID: "fit", State: raw(t, "same question, fit state"),
			Answer: raw(t, decidetest.ChoiceAnswer(valid)), Label: "o001",
		}},
	}
	hold := base
	hold.Cases = []Case{{
		ID: "heldout", State: raw(t, "same question, held-out state"),
		Answer: raw(t, decidetest.ChoiceAnswer(valid)), Label: "o001",
	}}
	c := Config{
		Measure: gate.MeasureConfidence, Polarity: gate.HighIsSafe,
		MaxError: 1, MinAllowed: 1, Temperatures: []float64{2}, ECEBins: 5,
	}
	zeroFit := base
	zeroFit.Cases = append([]Case(nil), base.Cases...)
	zeroFit.Cases[0].Answer = raw(t, zeroAnswer)
	if _, err := Fit(zeroFit, hold, c); !errors.Is(err, ErrInvalidDataset) ||
		!strings.Contains(err.Error(), "positive mass") {
		t.Fatalf("zero-mass fit: %v", err)
	}
	underweight := make(map[string]float64, len(valid))
	for key, p := range valid {
		underweight[key] = p
	}
	underweight["o000"] = 0.88 // sum 0.98: root accepts, calibration rejects.
	underweightAnswer := decidetest.ChoiceAnswer(underweight)
	if err := q.ValidateAnswer(underweightAnswer); err != nil {
		t.Fatalf("fixture no longer reproduces root tolerance: %v", err)
	}
	zeroFit.Cases[0].Answer = raw(t, underweightAnswer)
	if _, err := Fit(zeroFit, hold, c); !errors.Is(err, ErrInvalidDataset) ||
		!strings.Contains(err.Error(), "sum to") {
		t.Fatalf("underweight fit: %v", err)
	}
	a, err := Fit(base, hold, c)
	if err != nil || a.Temperature != 2 {
		t.Fatalf("nonidentity fit: temperature=%v, err=%v", a.Temperature, err)
	}
	candidate := hold
	candidate.Model = "jev-1.14.0"
	candidate.Cases = append([]Case(nil), hold.Cases...)
	candidate.Cases[0].Answer = raw(t, zeroAnswer)
	_, err = Compare(a, candidate, Tolerance{
		Metric: MetricAdjustedBrier, Direction: HigherIsWorse, MaxDelta: 0,
	})
	if !errors.Is(err, ErrInvalidDataset) || !strings.Contains(err.Error(), "positive mass") {
		t.Fatalf("zero-mass comparison: %v", err)
	}
}
