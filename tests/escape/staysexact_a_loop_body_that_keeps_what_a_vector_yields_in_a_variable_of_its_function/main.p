package main

import "pluto/vec"
var a, b int
func f() int {
	v := vec.New[*int]()
	defer v.Free()
	v.Push(&a)
	v.Push(&b)
	var last *int
	for _, p := range v.All() {
		last = p
	}
	return *last
}
func main() { println(f()) }
