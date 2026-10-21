package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type Putter interface{ Put(p *int) }
type I struct{}

func (*I) Put(p *int) { keep = p }

type W struct{ Putter }

func put[P Putter](p P, x *int) { p.Put(x) }

var gi I

//pu:noinline
func f() {
	x := 42
	put(W{&gi}, &x)
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
