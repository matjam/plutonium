package main

import (
	"pluto/alloc"
	"pluto/arena"
)
func main() {
	a := arena.NewFixed(256)
	r := alloc.Use(&a)
	p := new(int)
	*p = 1
	r.Restore()
}
