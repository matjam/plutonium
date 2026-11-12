package main

import (
	"pluto/arena"
	"pluto/sync"
)

func get(a *arena.Fixed) *int { return a.New[int]() }

func main() {
	a := arena.NewFixed(1024)
	p := get(&a)
	*p = 1
	var s sync.Scope
	s.Task(func() { a.Reset() })
	println(*p)
	s.Wait()
}
