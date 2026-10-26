package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/vec"
)
func f() vec.Vec[int] {
	heap := alloc.Default()
	ar := arena.NewFixed(256)
	defer alloc.Use(&ar).Restore()
	v := vec.New[int](heap)
	v.Push(1)
	return v
}
func main() { _ = f() }
