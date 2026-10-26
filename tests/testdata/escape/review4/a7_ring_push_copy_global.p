package main

import (
	"unsafe"
)

var g ring[*int, 4]
var mark int

//pu:noinline
func f() {
	var r ring[*int, 4]
	x := 42
	r.Push(&x) // stores &x (a frame pointer) into r's inline storage
	g = r      // copies the ring, &x included, into a package variable
}

//pu:noinline
func clobber(n int) int {
	var a [64]int
	for i := range a {
		a[i] = n + i
	}
	s := 0
	for i := range a {
		s += a[i]
	}
	return s
}

func main() {
	f()
	println(clobber(1000))
	p, ok := g.Pop()
	if !ok {
		println("empty")
		return
	}
	println("kept frame ptr below mark:", uintptr(unsafe.Pointer(p)) > uintptr(unsafe.Pointer(&mark)))
	println("value (42 if intact):", *p)
}
