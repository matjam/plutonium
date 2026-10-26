package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type A interface{ Put(p *int) }
type B interface {
	A
	Other()
}
type I struct{}

func (*I) Put(p *int) { keep = p }
func (*I) Other()	 {}

var gi I
var gb B = &gi

//pu:noinline
func f() {
	x := 42
	var a A = gb
	a.Put(&x)
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
