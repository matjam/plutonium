package main

import (
	"pluto/arena"
	"pluto/sync"
)

func main() {
	a := arena.NewFixed(1024)
	p := a.New[int]()
	*p = 1
	var s sync.Scope
	s.Task(func() { println(*p) })
	// b takes over a's memory, so its Reset frees *p while the task reads it.
	b := take(&a)
	b.Reset()
	s.Wait()
}
