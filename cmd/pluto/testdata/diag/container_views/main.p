package main

import "pluto/vec"

// A vector keeps its elements in heap memory, so a view of the frame may
// not be one of them, whether the vector is a package variable or a local
// one (spec section 18.3). The functions the compiler expands inline for
// the containers (rt.InitValue, rt.MoveValues, rt.CopyValue and the rest)
// are checked as what they do.

var all vec.Vec[*int]

// Global stores a pointer to a local variable in a package vector.
func Global() {
	x := 1
	all.Push(&x)
}

// Local stores one in a local vector, whose storage is on the heap.
func Local() *int {
	y := 2
	v := vec.New[*int]()
	defer v.Free()
	v.Push(&y)
	return v.At(0)
}

var heapArr [1]*int

type B struct{ p *[1]*int }

func (b *B) init() { b.p = &heapArr }

// Through stores through a field that init set to heap memory.
func Through() {
	var b B
	b.init()
	z := 3
	b.p[0] = &z
}

// Fine: the vector holds pointers to package variables.
var n1, n2 int

func Fine() int {
	v := vec.New[*int]()
	defer v.Free()
	v.Push(&n1)
	v.Push(&n2)
	return *v.At(1)
}

// makeLocal stores a local array in a B's p, so that p is not a field that
// only ever holds heap memory.
func makeLocal() int {
	var arr [1]*int
	b := B{&arr}
	return len(b.p)
}

func main() {
	Global()
	println(Local())
	Through()
	println(Fine())
	_ = makeLocal()
}
