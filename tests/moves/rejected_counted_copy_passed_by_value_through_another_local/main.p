package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// The copy given to the function is another local copy of the reference
// the view was obtained through (language spec section 18.3).

func grow(b shared[vec.Vec[int]]) {
	helper(b)
}

func helper(b shared[vec.Vec[int]]) {
	b.Push(1)
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	s := a.Slice()
	b := a
	grow(b)
	println(s[0])
}
