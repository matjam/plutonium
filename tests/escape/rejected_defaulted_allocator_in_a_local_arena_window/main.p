package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/vec"
)
func f() vec.Vec[int] {
	ar := arena.NewFixed(256)
	r := alloc.Use(&ar)
	v := vec.New[int]()
	r.Restore()
	return v
}
func main() { _ = f() }
