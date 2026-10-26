package main

import (
	"unsafe"
)

type L4 struct {
	a int
	p *int
}
type L3 struct {
	a int
	l L4
}
type L2 struct {
	a int
	l L3
}
type L1 struct {
	a int
	l L2
}

var keep *int

//pu:noinline
func g(p *L1) { keep = p.l.l.l.p }

//pu:noinline
func f() {
	x := 42
	var v L1
	v.l.l.l.p = &x
	g(&v)
}

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

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
