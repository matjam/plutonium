package main

import "pluto/vec"
var gv vec.Vec[*int]
func f() {
	x := 42
	gv.Push(&x)
}
func main() { f() }
