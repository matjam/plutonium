package main

import (
	"pluto/arena"
	"pluto/sync"
)

// An arena is named by its variable or field, as a lock is (language spec
// section 19.3, Frees are writes): a Reset of h.a frees every object
// h.a.New allocated, so the starter may not reset it while a scoped task
// reads one (section 19.4, E3404).

type Holder owner struct {
	a arena.Fixed
}

func main() {
	var h Holder
	h.a = arena.NewFixed(1024)
	p := h.a.New[int]()
	*p = 1
	var s sync.Scope
	s.Task(func() { println(*p) })
	h.a.Reset() // frees the int the task reads
	s.Wait()
}
