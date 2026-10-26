package main

import "pluto/rt"
var g *int
func f() {
	x := 42
	rt.InitValue(&g, &x)
}
func main() { f() }
