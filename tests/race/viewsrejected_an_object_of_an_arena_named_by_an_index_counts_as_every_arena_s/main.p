package main

import (
	"pluto/arena"
	"pluto/sync"
)

func main() {
	var arenas [2]arena.Fixed
	arenas[0] = arena.NewFixed(1024)
	arenas[1] = arena.NewFixed(1024)
	i := 1
	p := arenas[i].New[int]()
	*p = 1
	b := arena.NewFixed(1024)
	var s sync.Scope
	s.Task(func() { b.Reset() })
	println(*p)
	s.Wait()
}
