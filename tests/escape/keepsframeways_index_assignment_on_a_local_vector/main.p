package main

import "pluto/vec"
var keep *int
func f() {
	x := 42
	v := vec.Make[*int](1)
	defer v.Free()
	v[0] = &x
	keep = v[0]
}
func main() { f() }
