package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	a := arena.NewFixed(1024)
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		m := a.Mark()
		a.Release(m)
		mu.Unlock()
	})
	*p = 2
	s.Wait()
}
