package main

import (
	"unsafe"
)

var garr [1]*int

//pu:noinline
func g(dst []*int) {
	x := 42
	src := [1]*int{&x}
	copy(dst, src[:])
}

//pu:noinline
func f() { g(garr[:]) }

func kept() unsafe.Pointer { return unsafe.Pointer(garr[0]) }

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
