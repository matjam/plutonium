package lower_test

import (
	"testing"
	"time"
)

// TestClosureResultsOfSelfCallingLiteral checks that finding what a function
// literal returns ends when the literal returns a call of itself through the
// variable that holds it (the lowerer of the self-hosted compiler has such
// literals): the analysis looked for what the literal returns for as long as
// the stack allowed.
func TestClosureResultsOfSelfCallingLiteral(t *testing.T) {
	const src = `package main

func main() {
	var f func(n int) func() int
	f = func(n int) func() int {
		if n < 2 {
			return func() int { return n }
		}
		return f(n - 1)
	}
	println(f(5)())
}
`
	done := make(chan string, 1)
	go func() { done <- lowerModeError(t, src, hosted) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("lowering a literal that returns a call of itself did not finish in 60 seconds")
	}
}
