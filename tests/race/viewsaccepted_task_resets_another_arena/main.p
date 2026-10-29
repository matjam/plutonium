package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

func main() {
	a := arena.NewFixed(1024)
	b := arena.NewFixed(1024)
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		b.Reset()
		w := vec.New[int](&b)
		w.Push(7)
		println(w.Len())
	})
	println(*p)
	s.Wait()
}
