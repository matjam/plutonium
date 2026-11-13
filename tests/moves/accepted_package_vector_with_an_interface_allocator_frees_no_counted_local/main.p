package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// A vector in a package variable allocates through an interface, and the
// allocators of the program that may be held in it hold no counted
// reference: growing it frees nothing a local shared reference points to.
// The view of a, and of the local vector b, lives across the call
// (language spec section 18.3).

var log vec.Vec[int]

func note(i int) {
	for j := 0; j < 100; j++ {
		log.Push(i + j)
	}
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	s := a.Slice()
	b := vec.New[int]()
	b.Push(1)
	note(1)
	b.Push(2)
	println(s[0], b.Len(), log.Len())
}
