package main

import "pluto/arena"

// A view of what an arena holds, stored in memory the same arena holds,
// is freed with that memory, so dropping the arena is fine (language spec
// section 18.3).

type node struct {
	v    int
	next *node
}

func main() {
	a := arena.NewChunked(1024)
	n := a.New[node]()
	n.v = 1
	n.next = a.New[node]()
	n.next.v = 2
	println(n.v + n.next.v)
	a.Drop()
}
