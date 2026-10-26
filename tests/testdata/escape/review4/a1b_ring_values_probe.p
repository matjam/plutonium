package main

import (
	"unsafe"
	"pluto/iter"
)

var g iter.Seq2[int, *int]
var mark int

//pu:noinline
func f() {
	var r ring[*int, 4]
	x := 42
	r.Push(&x)
	println("in f, &x dangling check base:", uintptr(unsafe.Pointer(&x)) > uintptr(unsafe.Pointer(&mark)))
	g = r.All()
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
	count := 0
	var last *int
	for _, p := range g {
		count++
		last = p
	}
	println("iterations:", count)
	if last != nil {
		println("kept frame ptr below mark:", uintptr(unsafe.Pointer(last)) > uintptr(unsafe.Pointer(&mark)))
		println("value (42 if intact):", *last)
	}
}
