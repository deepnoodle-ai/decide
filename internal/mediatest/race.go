//go:build race

package mediatest

// Race is true when the race detector is on. It changes allocation
// counts, so allocation tests skip under it.
const Race = true
