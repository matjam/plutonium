package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)
func f(a, b string) {
	ar := arena.NewFixed(256)
	defer alloc.Use(&ar).Restore()
	s := a + b
	fmt.Println(s)
}
func main() { f("x", "y") }
