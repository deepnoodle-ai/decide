package gate_test

import (
	"errors"
	"math"
	"testing"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

func entropy(ps ...float64) float64 {
	h := 0.0
	for _, p := range ps {
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return 1 - h/math.Log(float64(len(ps)))
}

func TestMeasure(t *testing.T) {
	c3 := choice("c", 0.55, map[string]float64{"a": 0.7, "b": 0.2, "c": 0.1})
	s2 := gate.Input{Name: "s", Kind: gate.KindScore, Probabilities: map[string]float64{"0": 0.2, "1": 0.8},
		Confidence: 0.6, HasConfidence: true}
	noHasConf := c3
	noHasConf.HasConfidence = false
	inner := errors.New("boom")

	type tc struct {
		name   string
		in     gate.Input
		m      gate.Measure
		option string
		want   float64
		err    error
	}
	cases := []tc{
		// Noul column, p = 0.8.
		{"noul confidence", noul("n", 0.8), gate.MeasureConfidence, "", 0, gate.ErrUndefined},
		{"noul top", noul("n", 0.8), gate.MeasureTop, "", 0.8, nil},
		{"noul top low p", noul("n", 0.3), gate.MeasureTop, "", 0.7, nil},
		{"noul margin", noul("n", 0.8), gate.MeasureMargin, "", 0.6, nil},
		{"noul entropy", noul("n", 0.8), gate.MeasureEntropy, "", entropy(0.8, 0.2), nil},
		{"noul applicability high", noul("n", 0.8), gate.MeasureApplicability, "", 0.8, nil},
		{"noul applicability low", noul("n", 0.1), gate.MeasureApplicability, "", 0.9, nil},
		{"noul noul", noul("n", 0.8), gate.MeasureNoul, "", 0.8, nil},
		{"noul prob true", noul("n", 0.8), gate.MeasureProb, "true", 0.8, nil},
		{"noul prob false", noul("n", 0.8), gate.MeasureProb, "false", 0.2, nil},
		{"noul prob other", noul("n", 0.8), gate.MeasureProb, "yes", 0, gate.ErrUndefined},
		{"noul 0 ln 0", noul("n", 0), gate.MeasureEntropy, "", 1, nil},
		{"noul p=1 entropy", noul("n", 1), gate.MeasureEntropy, "", 1, nil},
		{"noul p=0.5 entropy", noul("n", 0.5), gate.MeasureEntropy, "", 0, nil},
		{"noul 0 is a reading", noul("n", 0), gate.MeasureNoul, "", 0, nil},
		// Choice column.
		{"choice confidence", c3, gate.MeasureConfidence, "", 0.55, nil},
		{"choice top", c3, gate.MeasureTop, "", 0.7, nil},
		{"choice margin", c3, gate.MeasureMargin, "", 0.5, nil},
		{"choice entropy", c3, gate.MeasureEntropy, "", entropy(0.7, 0.2, 0.1), nil},
		{"choice applicability", c3, gate.MeasureApplicability, "", 0, gate.ErrUndefined},
		{"choice noul", c3, gate.MeasureNoul, "", 0, gate.ErrUndefined},
		{"choice prob", c3, gate.MeasureProb, "b", 0.2, nil},
		{"choice prob absent", c3, gate.MeasureProb, "z", 0, gate.ErrUndefined},
		{"choice no confidence", noHasConf, gate.MeasureConfidence, "", 0, gate.ErrUndefined},
		{"choice no confidence top ok", noHasConf, gate.MeasureTop, "", 0.7, nil},
		// Score column.
		{"score confidence", s2, gate.MeasureConfidence, "", 0.6, nil},
		{"score top", s2, gate.MeasureTop, "", 0.8, nil},
		{"score margin", s2, gate.MeasureMargin, "", 0.6, nil},
		{"score entropy", s2, gate.MeasureEntropy, "", entropy(0.2, 0.8), nil},
		{"score applicability", s2, gate.MeasureApplicability, "", 0, gate.ErrUndefined},
		{"score noul", s2, gate.MeasureNoul, "", 0, gate.ErrUndefined},
		{"score prob", s2, gate.MeasureProb, "1", 0.8, nil},
		// k = 1.
		{"k1 margin", choice("c", 1, map[string]float64{"a": 1}), gate.MeasureMargin, "", 1, nil},
		{"k1 margin partial", choice("c", 1, map[string]float64{"a": 0.97}), gate.MeasureMargin, "", 0.97, nil},
		{"k1 entropy", choice("c", 1, map[string]float64{"a": 1}), gate.MeasureEntropy, "", 1, nil},
		{"k1 top", choice("c", 1, map[string]float64{"a": 1}), gate.MeasureTop, "", 1, nil},
		// 0 ln 0 in a distribution.
		{"choice 0 ln 0", choice("c", 1, map[string]float64{"a": 1, "b": 0, "c": 0}), gate.MeasureEntropy, "", 1, nil},
		// Sums near 1 are accepted; entropy is clamped.
		{"sum 0.99", choice("c", 0.5, map[string]float64{"a": 0.5, "b": 0.49}), gate.MeasureTop, "", 0.5, nil},
		{"sum 1.01", choice("c", 0.5, map[string]float64{"a": 0.51, "b": 0.5}), gate.MeasureMargin, "", 0.01, nil},
		{"sum 1.01 entropy", choice("c", 0.5, map[string]float64{"a": 0.51, "b": 0.5}), gate.MeasureEntropy, "", entropy(0.51, 0.5), nil},
		{"entropy clamped", choice("c", 0, map[string]float64{"a": 0.4, "b": 0.4}), gate.MeasureEntropy, "", 0, nil},
		{"tolerance above 1", noul("n", 1+1e-7), gate.MeasureNoul, "", 1 + 1e-7, nil},
		{"tolerance below 0", noul("n", -1e-7), gate.MeasureNoul, "", -1e-7, nil},
		// Bad values.
		{"noul NaN", noul("n", math.NaN()), gate.MeasureNoul, "", 0, gate.ErrBadValue},
		{"noul Inf", noul("n", math.Inf(1)), gate.MeasureTop, "", 0, gate.ErrBadValue},
		{"noul -0.1", noul("n", -0.1), gate.MeasureApplicability, "", 0, gate.ErrBadValue},
		{"noul 1.1", noul("n", 1.1), gate.MeasureProb, "true", 0, gate.ErrBadValue},
		{"prob NaN", choice("c", 0.5, map[string]float64{"a": math.NaN(), "b": 0.5}), gate.MeasureTop, "", 0, gate.ErrBadValue},
		{"prob Inf", choice("c", 0.5, map[string]float64{"a": math.Inf(-1), "b": 0.5}), gate.MeasureEntropy, "", 0, gate.ErrBadValue},
		{"prob -0.1", choice("c", 0.5, map[string]float64{"a": -0.1, "b": 0.5}), gate.MeasureProb, "b", 0, gate.ErrBadValue},
		{"prob 1.1", choice("c", 0.5, map[string]float64{"a": 1.1, "b": 0.5}), gate.MeasureMargin, "", 0, gate.ErrBadValue},
		{"confidence NaN", choice("c", math.NaN(), map[string]float64{"a": 1}), gate.MeasureConfidence, "", 0, gate.ErrBadValue},
		{"confidence 1.1", choice("c", 1.1, map[string]float64{"a": 1}), gate.MeasureConfidence, "", 0, gate.ErrBadValue},
		{"empty distribution", gate.Input{Name: "c", Kind: gate.KindChoice}, gate.MeasureTop, "", 0, gate.ErrBadValue},
		// Missing kinds and unknown measures.
		{"missing", gate.Missing("x"), gate.MeasureTop, "", 0, gate.ErrMissing},
		{"failed", gate.Failed("x", inner), gate.MeasureTop, "", 0, inner},
		{"failed nil err", gate.Failed("x", nil), gate.MeasureTop, "", 0, gate.ErrMissing},
		{"abstained", gate.Abstained("x"), gate.MeasureTop, "", 0, gate.ErrMissing},
		{"empty measure", c3, "", "", 0, gate.ErrUndefined},
		{"unknown measure", c3, "certainty", "", 0, gate.ErrUndefined},
		{"unknown measure noul", noul("n", 0.5), "certainty", "", 0, gate.ErrUndefined},
		{"unknown kind", gate.Input{Name: "x", Kind: 42}, gate.MeasureTop, "", 0, gate.ErrUndefined},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.in.Measure(c.m, c.option)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("err = %v, want %v", err, c.err)
				}
				if got != 0 {
					t.Fatalf("value %v with error", got)
				}
				return
			}
			if err != nil || !near(got, c.want) {
				t.Fatalf("got %v, %v; want %v", got, err, c.want)
			}
		})
	}
	// A failed input matches ErrMissing as well as its own error.
	if _, err := gate.Failed("x", inner).Measure(gate.MeasureTop, ""); !errors.Is(err, gate.ErrMissing) {
		t.Errorf("failed input does not match ErrMissing: %v", err)
	}
}
