package main

import (
	"pluto/alloc"
	"pluto/arena"
)
var ar arena.Chunked
func f(a, b string) string {
	defer alloc.Use(&ar).Restore()
	return a + b
}
func main() { _ = f("x", "y") }
