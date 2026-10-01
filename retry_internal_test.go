package sod

import (
	"math"
	"net/http"
	"testing"
	"time"
)

func TestBackoffFormula(t *testing.T) {
	initial, maxDelay := 500*time.Millisecond, 5*time.Second
	for k, base := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second} {
		hi := backoff(k, initial, maxDelay, 0)
		lo := backoff(k, initial, maxDelay, 0.9999999)
		if hi != base || lo < time.Duration(float64(base)*0.75) || lo > base {
			t.Errorf("k=%d: [%v, %v], want [0.75*%v, %v]", k, lo, hi, base, base)
		}
	}
	for _, k := range []int{40, 61, 62, 200} {
		if d := backoff(k, initial, maxDelay, 0); d != maxDelay {
			t.Errorf("k=%d: %v", k, d)
		}
	}
	// A huge initial must not wrap negative, nor may jitter overflow.
	if d := backoff(3, time.Duration(math.MaxInt64/4), time.Duration(math.MaxInt64/2), 0); d != time.Duration(math.MaxInt64/2) {
		t.Errorf("wrap: %v", d)
	}
	if d := backoff(0, time.Duration(math.MaxInt64), time.Duration(math.MaxInt64), 0); d != time.Duration(math.MaxInt64) {
		t.Errorf("max: %v", d)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		h    map[string]string
		want time.Duration
		ok   bool
	}{
		{map[string]string{"Retry-After": "2"}, 2 * time.Second, true},
		{map[string]string{"Retry-After": "0.5"}, 500 * time.Millisecond, true},
		{map[string]string{"Retry-After": "-1"}, 0, false},
		{map[string]string{"Retry-After": "soon"}, 0, false},
		{map[string]string{"Retry-After": now.Add(3 * time.Second).Format(http.TimeFormat)}, 3 * time.Second, true},
		{map[string]string{"Retry-After": now.Add(-time.Hour).Format(http.TimeFormat)}, 0, true},
		{map[string]string{"Retry-After": "9", "retry-after-ms": "120"}, 120 * time.Millisecond, true},
		{map[string]string{"Retry-After": "9", "retry-after-ms": "bad"}, 9 * time.Second, true},
		{map[string]string{"Retry-After": "1e300"}, time.Duration(math.MaxInt64), true},
		{map[string]string{"Retry-After": "1e10"}, time.Duration(math.MaxInt64), true},
		{map[string]string{"retry-after-ms": "1e300"}, time.Duration(math.MaxInt64), true},
		{nil, 0, false},
	}
	for _, c := range cases {
		h := http.Header{}
		for k, v := range c.h {
			h.Set(k, v)
		}
		got, ok := parseRetryAfter(h, now)
		if got != c.want || ok != c.ok {
			t.Errorf("%v: got %v, %v", c.h, got, ok)
		}
	}
}
