package main

import (
	"pluto/arena"
	"pluto/sync"
)

// Holder keeps the address of an arena in a heap object.
type Holder struct {
	a *arena.Fixed
}

func main() {
	a := arena.NewFixed(1024)
	h := &Holder{a: &a}
	p := a.New[int]()
	*p = 1
	var s sync.Scope
	// The task resets the arena through the heap object h points to only
	// after main has read *p.
	println(*p)
	s.Task(func() { h.a.Reset() })
	s.Wait()
}
