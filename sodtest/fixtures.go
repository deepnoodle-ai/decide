package sodtest

import (
	"embed"
	"maps"
	"path"
	"slices"
	"strconv"
	"testing"

	"github.com/deepnoodle-ai/sod"
)

// NoulAnswer returns a noul answer with P(yes) = p.
func NoulAnswer(p float64) *sod.NoulAnswer {
	return &sod.NoulAnswer{Noul: p}
}

// ChoiceAnswer sets Choice to the argmax (first key in sorted order on ties)
// and Confidence by Confidence(probs). Taking only the distribution means a
// fixture cannot be self-inconsistent; build a struct literal to test that.
func ChoiceAnswer(probs map[string]float64) *sod.ChoiceAnswer {
	keys := slices.Sorted(maps.Keys(probs))
	a := &sod.ChoiceAnswer{Probabilities: make(map[string]float64, len(probs))}
	ps := make([]float64, 0, len(keys))
	for _, k := range keys {
		p := probs[k]
		a.Probabilities[k] = p
		ps = append(ps, p)
		if a.Choice == "" || p > probs[a.Choice] {
			a.Choice = k
		}
	}
	a.Confidence = Confidence(ps)
	return a
}

// ChoiceFor is ChoiceAnswer with probabilities given in q's option order.
// It panics if len(probs) != len(q.Criteria).
func ChoiceFor(q *sod.ChoiceQuestion, probs ...float64) *sod.ChoiceAnswer {
	if len(probs) != len(q.Criteria) {
		panic("sodtest: ChoiceFor needs one probability per option")
	}
	m := make(map[string]float64, len(probs))
	for i, o := range q.Criteria {
		m[o.Key] = probs[i]
	}
	return ChoiceAnswer(m)
}

// ScoreAnswer builds legend and string-keyed probabilities from levels and
// probs (same length or it panics), Score = sum(i*p_i), Confidence as above.
func ScoreAnswer(levels []any, probs ...float64) *sod.ScoreAnswer {
	if len(levels) != len(probs) {
		panic("sodtest: ScoreAnswer needs one probability per level")
	}
	a := &sod.ScoreAnswer{
		Legend:        make(map[string]any, len(levels)),
		Probabilities: make(map[string]float64, len(probs)),
		Confidence:    Confidence(probs),
	}
	for i, p := range probs {
		k := strconv.Itoa(i)
		a.Legend[k] = levels[i]
		a.Probabilities[k] = p
		a.Score += float64(i) * p
	}
	return a
}

// Confidence is (n*max(p) - 1)/(n - 1), clamped to [0,1]; 1 when n == 1 and
// 0 when n == 0.
//
// Source: the ConfidenceExplorer widget in
// https://docs.typesafe.ai/confidence.md. TypeSafe has not published its
// formula as documentation. This is a stand-in for fixtures, not a claim
// about what the server computes.
func Confidence(probs []float64) float64 {
	n := len(probs)
	switch n {
	case 0:
		return 0
	case 1:
		return 1
	}
	top := probs[0]
	for _, p := range probs[1:] {
		top = max(top, p)
	}
	return min(max((float64(n)*top-1)/float64(n-1), 0), 1)
}

// Testdata holds example payloads: api_score_request.json and
// api_score_response.json are verbatim from https://docs.typesafe.ai/api;
// api_noul_response.json, api_choice_response.json, error_422.json, and
// error_auth.json follow the documented shapes.
//
//go:embed testdata
var Testdata embed.FS

// ReadFile reads name from the embedded Testdata FS; it fails the test if
// absent.
func ReadFile(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := Testdata.ReadFile(path.Join("testdata", name))
	if err != nil {
		tb.Fatalf("sodtest: %v", err)
	}
	return b
}
