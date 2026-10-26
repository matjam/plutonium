package main

import (
	"unsafe"
)

type Pair[A, B any] struct {
	a A
	b B
}

func first[A, B any](p Pair[A, B]) A { return p.a }

var keep *int

//pu:noinline
func g() *int {
	x := 42
	return first(Pair[*int, int]{&x, 1})
}

//pu:noinline
func f() { keep = g() }

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
