package main

import "pluto/arena"

// A local checked arena's allocations end at a Reset or Release made
// through a pointer to the arena, or by a function given one (language
// spec sections 18.3 and 18.9).

func wipe(a *arena.Chunked) { a.Reset() }

func wipeVia(a *arena.Chunked) { wipe(a) } // two calls deep

func main() {
	// A function that resets the arena it is given.
	a := arena.NewChunked(1024)
	p := a.New[int]()
	wipe(&a)
	println(*p)

	// Reset through a pointer to the arena.
	b := arena.NewChunked(1024)
	q := b.New[int]()
	pb := &b
	pb.Reset()
	println(*q)

	// Two calls deep.
	c := arena.NewChunked(1024)
	r := c.New[int]()
	wipeVia(&c)
	println(*r)
}
