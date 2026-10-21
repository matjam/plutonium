package main

import (
	"unsafe"
)

var keep *int
var mark int

//pu:noinline
func f() {
	var r ring[*int, 4]
	x := 42
	r.Push(&x)
	p, _ := r.Pop() // p is &x, a frame pointer
	keep = p
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
	if keep == nil {
		println("nothing kept")
		return
	}
	println("dangling:", uintptr(unsafe.Pointer(keep)) > uintptr(unsafe.Pointer(&mark)))
	println("value (42 if intact):", *keep)
}
