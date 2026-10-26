package main

import "pluto/alloc"
var G *int
func main() {
	x := 1
	p := &x
	if x > 0 {
		p = alloc.New[int]()
		G = p
	}
	_ = p
}
