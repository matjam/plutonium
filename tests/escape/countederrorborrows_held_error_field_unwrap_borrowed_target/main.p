package main

import (
	"pluto/alloc"
	"pluto/errors"
)

type E struct{ n int }

func (e *E) Error() string { return "error" }

// A wrapping error that holds its child in a field of type error: Unwrap
// returns what the wrapping error keeps alive, so a pointer target of
// errors.As may borrow it while the wrapping error lives (section 18.3).
type wrapError struct {
	msg string
	err error
}

func (e *wrapError) Error() string { return e.msg }
func (e *wrapError) Unwrap() error { return e.err }

func main() {
	w := alloc.Synced[wrapError]()
	w.msg = "wrap"
	w.err = alloc.Synced[E]()
	var e error = w
	var p *E
	if errors.As(e, &p) {
		println(p.n)
	}
}
