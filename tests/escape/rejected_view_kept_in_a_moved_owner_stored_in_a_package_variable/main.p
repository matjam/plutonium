package main

import "pluto/vec"

// A view kept in a local owner goes with the owner when it is moved
// (language spec section 18.3). Storing the owner in a package variable,
// directly or after a move, keeps the view past v's frame (E3002).

type O owner struct{ s []int }

var keep O

func direct() {
	v := vec.New[int]()
	v.Push(1)
	o := O{}
	o.s = v.Slice()
	keep = o
	v.Free()
}

func moved() {
	v := vec.New[int]()
	v.Push(1)
	o := O{}
	o.s = v.Slice()
	p := o
	keep = p
	v.Free()
}

func main() {
	direct()
	moved()
	println(keep.s[0])
}
