package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// A copy of the reference in a package variable may point to the object
// the view looks at: a call that frees through the package variable ends
// the view (language spec section 18.3).

var g shared[vec.Vec[int]]

func grow() {
	for i := 0; i < 100; i++ {
		g.Push(i)
	}
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	g = a
	s := a.Slice()
	grow()
	println(s[0])
}
