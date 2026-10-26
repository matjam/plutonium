package main

import (
	"unsafe"
)

type O[T any] owner struct{ v T }

var keep unsafe.Pointer

func (o *O[T]) Drop() { keep = unsafe.Pointer(o) }

//pu:noinline
func f() {
	var o O[int]
	o.v = 42
}

func kept() unsafe.Pointer { return keep }

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
	p := kept()
	if p == nil {
		println("nothing kept")
		return
	}
	println("dangling:", uintptr(p) < uintptr(unsafe.Pointer(&mark)))
	println(clobber(1000))
	println("value (42 if intact):", *(*int)(p))
}
