package main

import "pluto/rt"
var g *int
func f() {
	x := 42
	p := &x
	g = rt.CopyValue(&p)
}
func main() { f() }
