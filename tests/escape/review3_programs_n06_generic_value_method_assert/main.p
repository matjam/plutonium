package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type G[E any] struct {
	p *E
	n int
}

var keep unsafe.Pointer

func (g G[E]) String() string { keep = unsafe.Pointer(g.p); return "g" }

//pu:noinline
func call(a any) string { return a.(Stringer).String() }

//pu:noinline
func f() {
	x := 42
	v := G[int]{&x, 1}
	println(call(&v))
}

func kept() unsafe.Pointer { return keep }

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
