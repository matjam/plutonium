package main

import (
	"unsafe"
	"pluto/iter"
)

var g iter.Seq2[int, *int]

//pu:noinline
func f() {
	var r ring[*int, 4]
	x := 42
	r.Push(&x)
	// r.All() builds a closure holding &r (a frame local). Storing it in a
	// package variable keeps a view of this frame.
	g = r.All()
}

func kept() unsafe.Pointer {
	var out unsafe.Pointer
	for _, p := range g {
		out = unsafe.Pointer(p)
	}
	return out
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
	var mark int
	f()
	println(clobber(1000))
	p := kept()
	if p == nil {
		println("nothing kept")
		return
	}
	println("dangling:", uintptr(p) < uintptr(unsafe.Pointer(&mark)))
	println("value (42 if intact):", *(*int)(p))
}
