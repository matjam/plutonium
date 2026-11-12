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
	// The task frees *p while main reads it.
	s.Task(func() { a.Reset() })
	println(*p)
	s.Wait()
}
