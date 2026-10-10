// Package calc is a fixture for the pluto test end-to-end test.
package calc

// Add returns a + b.
func Add(a, b int) int { return a + b }

// Div returns a / b; it panics when b is zero.
func Div(a, b int) int { return a / b }
