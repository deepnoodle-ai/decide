// Package fanout runs independent System One requests over a collection of
// items with a bound on concurrent work.
//
// This package runs many requests. TypeSafe's speculative fan-out pattern
// asks several questions in one request about one state; use the root
// request or patterns/heads for that pattern.
package fanout
