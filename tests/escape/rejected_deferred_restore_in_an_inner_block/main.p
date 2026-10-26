package main

import (
	"pluto/alloc"
	"pluto/arena"
)
func f(a, b string) string {
	ar := arena.NewFixed(256)
	{
		defer alloc.Use(&ar).Restore() // runs when f returns
	}
	return a + b
}
func main() { _ = f("x", "y") }
