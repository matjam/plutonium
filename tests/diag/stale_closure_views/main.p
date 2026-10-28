package main

import "pluto/vec"

// A function literal holds the views that the local variables it uses hold
// where it is made, so calling it after the owner moved uses a stale view
// (spec 18.3).

func consume(v vec.Vec[int]) {}

func main() {
	v := vec.New[int]()
	v.Push(1)
	s := v.Slice()
	g := func() int { return s[0] }
	consume(v)
	println(g())

	// Fine: called while the owner holds its value.
	w := vec.New[int]()
	w.Push(2)
	t := w.Slice()
	h := func() int { return t[0] }
	println(h())
	consume(w)

	// Fine: the literal uses the owner itself, not a view.
	u := vec.New[int]()
	u.Push(3)
	k := func() int { return u.Len() }
	println(k())
}
