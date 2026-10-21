package main

import (
	"unsafe"
	"pluto/vec"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var gv vec.Vec[*int]

//pu:noinline
func f() {
	x := 42
	var y int
	y, gv[0] = 1, &x
	_ = y
}

func init() { gv.Push(nil) }

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
