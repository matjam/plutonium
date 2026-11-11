package main

import "pluto/vec"

// A call of a func value frees what its candidates free through the
// argument; one whose candidates are not known frees through everything
// it is given (language spec section 18.3, A local owner's memory).

func run(f func(*vec.Vec[int]), v *vec.Vec[int]) { f(v) }

func push(v *vec.Vec[int]) { v.Push(1) }

var hook func(*vec.Vec[int])

func main() {
	// The candidate is known: push frees through v.
	v := vec.New[int]()
	v.Push(0)
	s := v.Slice()
	run(push, &v)
	println(s[0])

	// A func value read from a package variable: nothing is known.
	w := vec.New[int]()
	w.Push(0)
	t := w.Slice()
	if hook != nil {
		hook(&w)
	}
	println(t[0])
}
