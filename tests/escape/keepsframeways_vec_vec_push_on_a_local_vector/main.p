package main

import "pluto/vec"
var keep *int
func f() {
	x := 42
	v := vec.New[*int]()
	v.Push(&x)
	keep = v.At(0)
}
func main() { f() }
