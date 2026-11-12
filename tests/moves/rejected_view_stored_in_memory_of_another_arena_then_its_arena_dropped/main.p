package main

import "pluto/arena"

// A view of what arena a holds, stored in memory arena b holds, outlives
// a when a is dropped while b lives on (language spec section 18.3).

type node struct {
	v    int
	next *node
}

func main() {
	a := arena.NewChunked(1024)
	b := arena.NewChunked(1024)
	m := b.New[node]()
	m.next = a.New[node]()
	m.next.v = 2
	a.Drop()
	println(b.Used())
	b.Drop()
}
