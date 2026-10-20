package main

import (
	"unsafe"
	"pluto/sync/atomic"
)

var gp atomic.Pointer[int]

//pu:noinline
func f() {
	x := 42
	gp.Store(&x)
}

func kept() unsafe.Pointer { return unsafe.Pointer(gp.Load()) }

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
