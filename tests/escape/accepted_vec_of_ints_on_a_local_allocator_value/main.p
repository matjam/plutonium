package main

import (
	"pluto/alloc"
	"pluto/vec"
)
func main() {
	v := vec.New[int](alloc.Default())
	v.Push(1)
	v.Free()
}
