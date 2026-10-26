package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var gx int

type H interface{ Take(a, b *int) }
type T struct{}

func (T) Take(a, b *int) { keep = b }

func two(p *int) (*int, *int) { return &gx, p }

var hh H = T{}

//pu:noinline
func f() {
	x := 42
	hh.Take(two(&x))
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
