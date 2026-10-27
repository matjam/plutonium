package main

import (
	"pluto/errors"
	"pluto/fmt"
)

type detail struct{ line int }

type syntaxErr struct{ d *detail }

func (e syntaxErr) Error() string { return "syntax" }

var last = detail{3}

func parse() error { return syntaxErr{&last} }

// A value target stores a copy of the error found (section 16.6); it does
// not borrow the temporary counted error that wraps it.
func main() {
	var se syntaxErr
	ok := errors.As(fmt.Errorf("wrap: %w", parse()), &se)
	println(ok, se.d.line)
	if e, ok := errors.AsType[syntaxErr](fmt.Errorf("wrap: %w", parse())); ok {
		println(e.d.line)
	}
}
