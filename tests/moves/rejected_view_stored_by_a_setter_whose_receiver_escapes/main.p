package main

// set stores the slice into its receiver and also keeps the receiver in a
// package variable, so the view of v the slice is reaches memory the
// compiler does not follow. v.Push(3) may move v's elements while that
// view is kept (language spec section 18.3).

import "pluto/vec"

type H struct{ p []int }

var (
	last *H
	gh   H
)

func (h *H) set(p []int) {
	h.p = p
	last = h
}

func main() {
	v := vec.New[int]()
	v.Push(1)
	v.Push(2)
	g := &gh
	g.set(v.Slice())
	v.Push(3)
	println(v.Len())
}
