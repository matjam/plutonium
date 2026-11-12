package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// A counted reference passed by value is a copy that points to the same
// object: a function that frees through its copy ends the views obtained
// through the caller's (language spec section 18.3).

func grow(b shared[vec.Vec[int]]) {
	for i := 0; i < 100; i++ {
		b.Push(i)
	}
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	s := a.Slice()
	grow(a)
	println(s[0])
}
