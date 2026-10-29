package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

func reset(a *arena.Fixed) { a.Reset() }

func main() {
	var mu sync.Mutex
	a := arena.NewFixed(1024)
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		reset(&a)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}
