package main

import "pluto/vec"

// The loop body panics: the trace goes from the body, a function of main
// named main.main-range1, through the closure of vec's All iterator, named
// after All, to main.
func main() {
	v := vec.New[int]()
	defer v.Free()
	v.Push(10)
	v.Push(20)
	var small [2]int
	for i, x := range v.All() {
		println(i, x)
		small[x/10+1]++
	}
}
