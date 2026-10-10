// Package crash is a fixture whose test panics, ending the test binary.
package crash

// Div returns a / b; it panics when b is zero.
func Div(a, b int) int { return a / b }
