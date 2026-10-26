package main

import (
	"unsafe"
	"pluto/vec"
)

type U union {
	None
	P *int
}

var gv vec.Vec[U]

//pu:noinline
func f() {
	x := 42
	gv.Push(U.P(&x))
}

func kept() unsafe.Pointer {
	if p, ok := gv.Slice()[0].(U.P); ok {
		return unsafe.Pointer(p)
	}
	return nil
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
