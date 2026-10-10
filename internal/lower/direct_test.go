package lower_test

import (
	"testing"
)

// TestDirectStructNotBoxed checks that converting a struct or array that is
// a single pointer to an interface stores the pointer in the data word
// instead of boxing the value in the frame, and that a larger struct is
// still boxed.
func TestDirectStructNotBoxed(t *testing.T) {
	prog := lowerSource(t, `package main

type detail struct{ op string }

type wrapErr struct{ d *detail }

func (e wrapErr) Error() string { return e.d.op }

type handle [1]*detail

func (h handle) Error() string { return h[0].op }

type pair struct{ d, e *detail }

func (p pair) Error() string { return p.d.op }

var d detail

//pu:noinline
func direct() error { return wrapErr{&d} }

//pu:noinline
func array() error { return handle{&d} }

//pu:noinline
func boxed() error {
	var e error = pair{&d, &d}
	println(e.Error())
	return nil
}

//pu:noinline
func back(e error) *detail { return e.(wrapErr).d }

func main() {
	println(direct().Error(), array().Error(), boxed() == nil, back(direct()) == &d)
}
`)
	boxes := map[string]int{}
	for _, f := range prog.Funcs {
		for _, s := range f.Slots {
			if s.Name == "box" {
				boxes[f.Name]++
			}
		}
	}
	for _, name := range []string{"main.direct", "main.array", "main.back"} {
		if boxes[name] != 0 {
			t.Errorf("%s boxes its interface value (%d box slots)", name, boxes[name])
		}
	}
	if boxes["main.boxed"] == 0 {
		t.Error("main.boxed: a two-pointer struct is not boxed in the frame")
	}
}
