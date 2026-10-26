package main

import (
	"unsafe"
	"pluto/vec"
)

var gv vec.Vec[*int]

//pu:noinline
func g() vec.Vec[*int] {
	x := 42
	v := vec.New[*int]()
	v.Push(&x)
	return v
}

//pu:noinline
func f() { gv = g() }

func kept() unsafe.Pointer { return unsafe.Pointer(gv.Slice()[0]) }

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
