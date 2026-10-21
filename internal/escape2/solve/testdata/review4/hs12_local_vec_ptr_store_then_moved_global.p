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
	v := vec.New[*int]()
	v.Push(nil)
	p := v.Ptr(0)
	*p = &x
	gv = v
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
	p := kept()
	if p == nil {
		println("nothing kept")
		return
	}
	println("dangling:", uintptr(p) < uintptr(unsafe.Pointer(&mark)))
	println(clobber(1000))
	println("value (42 if intact):", *(*int)(p))
}
