package main

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/fmt"
)

type E struct{ n int }

func (e *E) Error() string { return "error" }

func makeError() error { return alloc.Synced[E]() }

// A *T target borrows the object of the counted error it matches (section
// 16.6), which the temporary keeps alive only until the end of its
// statement.
func main() {
	var p *E
	errors.As(fmt.Errorf("wrap: %w", makeError()), &p)
	println(p.n)
}
