package main

import "pluto/rt"
var g [1]*int
func f() {
	x := 42
	p := &x
	rt.MoveValues(&g[0], &p, 1)
}
func main() { f() }
