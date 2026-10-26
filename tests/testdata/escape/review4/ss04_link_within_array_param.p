package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type S struct{ p *int }

func link(a *[2]*S) { a[0].p = a[1].p }

//pu:noinline
func f() {
	x := 42
	var s0, s1 S
	s1.p = &x
	a := [2]*S{&s0, &s1}
	link(&a)
	keep = s0.p
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
