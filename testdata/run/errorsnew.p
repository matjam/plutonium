package main

import (
	"pluto/errors"
	"pluto/fmt"
)

// Each evaluation of errors.New is a new error, as in Go (language spec
// section 16.3): two results of one call site differ, a copy of a result is
// equal to it, and sentinels are evaluated once.

var (
	ErrA = errors.New("a")
	ErrB = errors.New("a") // same message, different call site
)

func header() error { return errors.New("bad header") }

func other() error { return errors.New("bad header") }

func pick(i int) error {
	if i%2 == 0 {
		return errors.New("even")
	}
	return errors.New("odd")
}

// Every instance of a generic function shares the call site.
func tagged[T any](x T) error { return errors.New("tagged") }

type wrap struct{ err error }

func (w *wrap) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrap) Unwrap() error { return w.err }

func main() {
	fmt.Println(ErrA, ErrB, ErrA == ErrB, ErrA == ErrA)

	e1, e2 := header(), header()
	e3 := e1
	fmt.Println(e1, e1 == e2, e1 == e3, errors.Is(e3, e1), errors.Is(e2, e1))
	fmt.Println(header() == other())

	// Many evaluations of one site are all different.
	seen := map[error]bool{}
	for i := 0; i < 1000; i++ {
		seen[header()] = true
	}
	fmt.Println(len(seen))

	fmt.Println(pick(0), pick(1), pick(2) == pick(4), pick(0) == pick(1))
	fmt.Println(tagged(1) == tagged("s"), tagged(2.5))

	// A new error can be wrapped and found again, and is a map key.
	w := &wrap{err: e1}
	fmt.Println(errors.Is(w, e1), errors.Is(w, e2), errors.Unwrap(w) == e1, w)
	m := map[error]string{e1: "one", e2: "two"}
	fmt.Println(len(m), m[e1], m[e3], m[e2])

	fmt.Printf("%T %T %v %s\n", e1, ErrA, e1, ErrB)
}
