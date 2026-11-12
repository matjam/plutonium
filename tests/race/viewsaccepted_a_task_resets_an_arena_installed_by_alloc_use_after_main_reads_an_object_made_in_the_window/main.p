package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/sync"
)

func main() {
	a := arena.NewFixed(1024)
	r := alloc.Use(&a)
	// new allocates from a, the default allocator inside the window.
	p := new(int)
	*p = 1
	r.Restore()
	var s sync.Scope
	println(*p)
	// The task frees *p only after main has read it.
	s.Task(func() { a.Reset() })
	s.Wait()
}
