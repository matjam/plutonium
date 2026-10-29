package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

type holder struct {
	a *arena.Fixed
}

// The task resets the arena through a field of a value of its own.
func main() {
	var mu sync.Mutex
	a := arena.NewFixed(1024)
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		h := holder{a: &a}
		h.a.Reset()
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}
