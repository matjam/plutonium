package main

import (
	"pluto/arena"
	"pluto/vec"
)

// Dropping v frees its buffer into the arena, which is dropped first.
func main() {
	var v vec.Vec[int]
	a := arena.NewFixed(4096)
	v = vec.New[int](&a)
	v.Push(1)
	println(v.Len())
}
