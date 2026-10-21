package main

import (
	"unsafe"
	"pluto/hash"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var gm hash.Map[*int, int]

//pu:noinline
func f() {
	x := 42
	gm[&x] = 1
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
