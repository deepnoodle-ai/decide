package calibrate

import (
	"maps"
	"math"
	"slices"

	"github.com/deepnoodle-ai/decide/patterns/gate"
)

// Metrics describes one held-out cohort under a frozen cutoff and
// temperature. ErrorRate is nil when no cases were allowed.
type Metrics struct {
	Count         int      `json:"count"`
	Allowed       int      `json:"allowed"`
	Errors        int      `json:"errors"`
	Coverage      float64  `json:"coverage"`
	ErrorRate     *float64 `json:"error_rate"`
	RawBrier      float64  `json:"raw_brier"`
	AdjustedBrier float64  `json:"adjusted_brier"`
	RawECE        float64  `json:"raw_ece"`
	AdjustedECE   float64  `json:"adjusted_ece"`
}

type bin struct {
	count     int
	probSum   float64
	actualSum float64
}

func (b *bin) add(p float64, actual bool) {
	b.count++
	b.probSum += p
	if actual {
		b.actualSum++
	}
}

func ece(bins []bin, n int) float64 {
	var result float64
	for _, b := range bins {
		if b.count == 0 {
			continue
		}
		result += math.Abs(b.probSum-b.actualSum) / float64(n)
	}
	return result
}

func binIndex(p float64, count int) int {
	return min(int(p*float64(count)), count-1)
}

func brier(s sample, probs map[string]float64, kind gate.Kind) float64 {
	if kind == gate.KindNoul {
		y := 0.0
		if s.label == "true" {
			y = 1
		}
		return math.Pow(probs["true"]-y, 2)
	}
	var sum float64
	for _, key := range slices.Sorted(maps.Keys(probs)) {
		y := 0.0
		if key == s.label {
			y = 1
		}
		sum += math.Pow(probs[key]-y, 2)
	}
	return sum / float64(len(probs))
}

func event(s sample, probs map[string]float64, kind gate.Kind) (float64, bool) {
	if kind == gate.KindNoul {
		return probs["true"], s.label == "true"
	}
	return probs[s.predicted], s.predicted == s.label
}

func evaluate(ms []measured, kind gate.Kind, c Config, cutoff, temp float64) Metrics {
	out := Metrics{Count: len(ms)}
	rawBins := make([]bin, c.ECEBins)
	adjBins := make([]bin, c.ECEBins)
	for _, m := range ms {
		s := m.sample
		if allowed(m.value, cutoff, c.Polarity) {
			out.Allowed++
			if wrong(s, c, kind) {
				out.Errors++
			}
		}
		adj := temperatureDistribution(s.probs, temp)
		out.RawBrier += brier(s, s.probs, kind)
		out.AdjustedBrier += brier(s, adj, kind)
		rp, ry := event(s, s.probs, kind)
		ap, ay := event(s, adj, kind)
		rawBins[binIndex(rp, c.ECEBins)].add(rp, ry)
		adjBins[binIndex(ap, c.ECEBins)].add(ap, ay)
	}
	out.Coverage = float64(out.Allowed) / float64(out.Count)
	if out.Allowed > 0 {
		rate := float64(out.Errors) / float64(out.Allowed)
		out.ErrorRate = &rate
	}
	out.RawBrier /= float64(out.Count)
	out.AdjustedBrier /= float64(out.Count)
	out.RawECE = ece(rawBins, out.Count)
	out.AdjustedECE = ece(adjBins, out.Count)
	return out
}

func temperatureDistribution(probs map[string]float64, temp float64) map[string]float64 {
	out := make(map[string]float64, len(probs))
	if temp == 1 {
		for k, p := range probs {
			out[k] = p
		}
		return out
	}
	maxLog := math.Inf(-1)
	for _, p := range probs {
		if p > 0 {
			maxLog = max(maxLog, math.Log(p))
		}
	}
	var total float64
	for _, k := range slices.Sorted(maps.Keys(probs)) {
		p := probs[k]
		if p == 0 {
			out[k] = 0
			continue
		}
		difference := math.Log(p) - maxLog
		weight := 1.0
		if difference != 0 {
			weight = math.Exp(difference / temp)
		}
		out[k] = weight
		total += weight
	}
	for k := range out {
		out[k] /= total
	}
	return out
}

func logLoss(ss []sample, temp float64) float64 {
	var loss float64
	for _, s := range ss {
		p := temperatureDistribution(s.probs, temp)[s.label]
		if p <= 0 {
			return math.Inf(1)
		}
		loss -= math.Log(p)
	}
	return loss / float64(len(ss))
}

func fitTemperature(ss []sample, grid []float64) float64 {
	options := append([]float64{1}, grid...)
	slices.Sort(options)
	options = slices.Compact(options)
	best := 1.0
	bestLoss := logLoss(ss, best)
	for _, temp := range options {
		loss := logLoss(ss, temp)
		if loss < bestLoss || (loss == bestLoss && best != 1 && temp < best) {
			best, bestLoss = temp, loss
		}
	}
	return best
}
