package main

import "pluto/alloc"
type Pair struct {
	a *int
	b *int
}
var G *int
func main() {
	x := 1
	p := Pair{a: &x, b: alloc.New[int]()}
	G = p.b
}
