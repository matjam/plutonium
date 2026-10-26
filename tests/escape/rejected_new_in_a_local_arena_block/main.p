package main

import (
	"pluto/alloc"
	"pluto/arena"
)
var G *int
func f() {
	ar := arena.NewFixed(256)
	{
		r := alloc.Use(&ar)
		p := new(int)
		G = (*int)(p)
		r.Restore()
	}
}
func main() { f() }
