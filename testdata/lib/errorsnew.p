package main

import (
	"pluto/errors"
	"pluto/fmt"
)

// errors.New with a constant message is a static error per call site
// (language spec section 14.1).

var (
	ErrA = errors.New("a")
	ErrB = errors.New("a") // same message, different call site
)

const msg = "bad header"

func header() error { return errors.New(msg) }

func other() error { return errors.New(msg) }

func pick(i int) error {
	if i%2 == 0 {
		return errors.New("even")
	}
	return errors.New("odd")
}

// Every instance of a generic function shares the call site's error.
func tagged[T any](x T) error { return errors.New("tagged") }

type wrap struct{ err error }

func (w *wrap) Error() string { return "wrapped: " + "x" }
func (w *wrap) Unwrap() error { return w.err }

var w = &wrap{err: errors.New("inner")}

func main() {
	fmt.Println(ErrA, ErrB, ErrA == ErrB, ErrA == ErrA)

	// The same call site returns the same error every time, as often as
	// it runs.
	first := header()
	same := true
	for i := 0; i < 1000; i++ {
		if header() != first {
			same = false
		}
	}
	fmt.Println(first, same, header() == other(), errors.Is(header(), first))

	fmt.Println(pick(0), pick(1), pick(2) == pick(4), pick(0) == pick(1))
	fmt.Println(tagged(1) == tagged("s"), tagged(2.5))

	// A static error can be wrapped and found again.
	fmt.Println(errors.Is(w, w.err), errors.Unwrap(w) == w.err, w.err)

	var t *errors.Text
	fmt.Println(errors.As(first, &t), t.Msg)
}
