package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// The function copies the package variable to a local and frees through
// the copy, two calls down (language spec section 18.3).

var g shared[vec.Vec[int]]

func current() shared[vec.Vec[int]] { return g }

func grow() {
	b := current()
	b.Push(1)
}

func outer() { grow() }

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	g = a
	s := a.Slice()
	outer()
	println(s[0])
}
