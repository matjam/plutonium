package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type R interface{ IndexOk(i int) (*int, bool) }
type C struct{ n int }

func (c *C) IndexOk(i int) (*int, bool) { return &c.n, true }

type W struct{ R }

//pu:noinline
func g() *int {
	c := C{42}
	var r R = W{&c}
	p, _ := r[0]
	return p
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
