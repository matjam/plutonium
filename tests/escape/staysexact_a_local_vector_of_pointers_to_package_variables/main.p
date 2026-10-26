package main

import "pluto/vec"
var a, b int
func f() int {
	v := vec.New[*int]()
	v.Push(&a)
	v.Push(&b)
	p := v.At(1)
	x := 3
	q := &x
	return *p + *q + v.Len()
}
func main() { println(f()) }
