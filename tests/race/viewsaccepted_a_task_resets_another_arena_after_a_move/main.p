package main

import (
	"pluto/arena"
	"pluto/sync"
)

func main() {
	a := arena.NewFixed(1024)
	c := arena.NewFixed(1024)
	p := a.New[int]()
	*p = 1
	var s sync.Scope
	s.Task(func() { println(*p) })
	// b takes over c's memory, which *p is not in.
	b := take(&c)
	b.Reset()
	s.Wait()
}
