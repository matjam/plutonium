package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)
func f(a, b string) {
	ar := arena.NewFixed(256)
	{
		r := alloc.Use(&ar)
		_ = a + b
		r.Restore()
	}
	fmt.Println(a + b) // made after the window: not in the arena
}
func main() { f("x", "y") }
