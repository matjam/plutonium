package main

import (
	"unsafe"
	"pluto/alloc"
)

type H struct{ p *int }

var gs shared[H]

//pu:noinline
func f() {
	x := 42
	s := alloc.Shared[H]()
	s.p = &x
	gs = s
}

func kept() unsafe.Pointer { return unsafe.Pointer(gs.p) }

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
