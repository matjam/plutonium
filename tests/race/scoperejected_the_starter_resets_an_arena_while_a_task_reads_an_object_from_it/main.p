package main

import (
	"pluto/arena"
	"pluto/sync"
)

// A Reset frees every object New allocated in the arena (language spec
// section 19.3, Frees are writes), so the starter may not reset the arena
// while a scoped task reads one (section 19.4, E3404).

func main() {
	a := arena.NewFixed(1024)
	p := a.New[int]()
	*p = 1
	var s sync.Scope
	s.Task(func() { println(*p) })
	a.Reset() // frees the int the task reads
	s.Wait()
}
