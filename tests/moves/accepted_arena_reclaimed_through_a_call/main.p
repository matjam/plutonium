package main

import "pluto/arena"

// A function given a local arena that does not reset or release it leaves
// the allocations alone, and allocations made after a reset through a
// pointer are fine (language spec sections 18.3 and 18.9).

func wipe(a *arena.Chunked) { a.Reset() }

func used(a *arena.Chunked) int { return a.Used() }

func main() {
	a := arena.NewChunked(1024)
	p := a.New[int]()
	println(used(&a))
	println(*p)
	wipe(&a)
	p = a.New[int]()
	println(*p)

	b := arena.NewChunked(1024)
	pb := &b
	pb.Reset()
	q := b.New[int]()
	println(*q)
}
