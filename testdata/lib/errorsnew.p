package main

import (
	"pluto/errors"
	"pluto/fmt"
)

// errors.New with a constant message makes an error of the call site's own
// type, with a new count for each evaluation (language spec section 16.3).
// testdata/run/errorsnew.p checks the same against Go.

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

// Every instance of a generic function shares the call site's type.
func tagged[T any](x T) error { return errors.New("tagged") }

type wrap struct{ err error }

func (w *wrap) Error() string { return "wrapped: " + "x" }
func (w *wrap) Unwrap() error { return w.err }

var w = &wrap{err: errors.New("inner")}

func main() {
	fmt.Println(ErrA, ErrB, ErrA == ErrB, ErrA == ErrA)

	// Each evaluation of a call site is a new error; a copy is equal.
	first := header()
	same := false
	for i := 0; i < 1000; i++ {
		if header() == first {
			same = true
		}
	}
	copied := first
	fmt.Println(first, same, header() == other(), first == copied, errors.Is(copied, first))

	fmt.Println(pick(0), pick(1), pick(2) == pick(4), pick(0) == pick(1))
	fmt.Println(tagged(1) == tagged("s"), tagged(2.5))

	// A new error can be wrapped and found again.
	fmt.Println(errors.Is(w, w.err), errors.Unwrap(w) == w.err, w.err)

	// The result is not an *errors.Text, which is the type of static
	// sentinel literals.
	var t *errors.Text
	fmt.Println(errors.As(first, &t))
	var lit error = &errors.Text{Msg: "literal"}
	fmt.Println(errors.As(lit, &t), t.Msg)
	fmt.Printf("%T\n", first)
}
