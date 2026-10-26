package main

import (
	"unsafe"
)

type H struct{ p *int }

var gh owned[H]

//pu:noinline
func f() {
	x := 42
	b := new(H)
	b.p = &x
	gh = b
}

func kept() unsafe.Pointer { return unsafe.Pointer(gh.p) }

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
