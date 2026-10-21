package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type R interface{ Index(i int) *int }
type C[T any] struct{ n int }

func (c *C[T]) Index(i int) *int { return &c.n }

//pu:noinline
func g() *int {
	c := C[string]{42}
	var r R = &c
	return r[0]
}

//pu:noinline
func f() { keep = g() }

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
