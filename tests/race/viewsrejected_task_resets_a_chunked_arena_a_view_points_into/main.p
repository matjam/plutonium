package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	a := arena.NewChunked()
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		a.Reset()
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}
