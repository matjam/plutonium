package main

import (
	"pluto/arena"
	"pluto/sync"
)

// A Release counts as freeing every object of the arena, since the
// compiler cannot order objects against marks (language spec section
// 19.3, Frees are writes; section 19.4, E3404).

func main() {
	a := arena.NewChunked(1024)
	m := a.Mark()
	xs := a.Slice[int](4)
	xs[0] = 1
	var s sync.Scope
	s.Task(func() { println(xs[0]) })
	a.Release(m) // frees the slice the task reads
	s.Wait()
}
