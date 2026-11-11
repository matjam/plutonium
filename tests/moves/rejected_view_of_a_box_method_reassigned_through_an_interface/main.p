package main

// A view a method returns is bounded by its holder, a box value, a type
// parameter value or an interface value alike (section 18.3, item 208).
// The interface case once made the compiler panic while lowering the itab
// adapter of shared[vec.Vec[int]]: the adapter took no type arguments
// through the box.

import (
	"pluto/alloc"
	"pluto/vec"
)

type S interface{ Slice() []int }

func plainShared(t shared[vec.Vec[int]]) {
	s := t.Slice()
	t = nil
	println(len(s))
}

func viaIface(t S, z S) {
	s := t.Slice()
	t = z
	println(len(s))
}

func viaParam[T S](t T) {
	s := t.Slice()
	var z T
	t = z
	println(len(s))
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(0)
	plainShared(a)
	b := alloc.Shared[vec.Vec[int]]()
	viaParam(b)
	c := alloc.Shared[vec.Vec[int]]()
	viaIface(c, nil)
}
