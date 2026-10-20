package main

import (
	"unsafe"
)

var gf func() int

func init() {
	x := 42
	gf = func() int { return x }
}

//pu:noinline
func f() {}

func kept() unsafe.Pointer { println(clobber(9)); println("closure returns", gf()); return nil }

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
