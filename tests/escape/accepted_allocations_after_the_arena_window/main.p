package main

import (
	"pluto/alloc"
	"pluto/arena"
)
func g(a, b string) string {
	ar := arena.NewFixed(256)
	{
		r := alloc.Use(&ar)
		_ = a + b
		r.Restore()
	}
	return a + b // after the window's block
}
func main() { _ = g("x", "y") }
