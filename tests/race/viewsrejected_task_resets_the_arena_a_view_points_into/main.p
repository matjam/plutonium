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
		a.Reset()
		w := vec.New[int](&a)
		w.Push(7)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}
