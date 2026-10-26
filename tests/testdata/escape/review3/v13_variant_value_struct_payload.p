package main

import (
	"unsafe"
)

type S struct {
	p *int
	n int
}
type U union {
	None
	V S
}

var keep U

//pu:noinline
func g() U {
	x := 42
	mk := U.V
	return mk(S{&x, 1})
}

//pu:noinline
func f() { keep = g() }

func kept() unsafe.Pointer {
	if s, ok := keep.(U.V); ok {
		return unsafe.Pointer(s.p)
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
