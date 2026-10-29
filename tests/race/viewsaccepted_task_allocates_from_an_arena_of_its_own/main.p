package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

func main() {
	a := arena.NewFixed(1024)
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		b := arena.NewFixed(1024)
		w := vec.New[int](&b)
		w.Push(7)
		b.Reset()
		w2 := vec.New[int](&b)
		w2.Push(8)
		println(w2.Len())
	})
	println(*p)
	s.Wait()
}
