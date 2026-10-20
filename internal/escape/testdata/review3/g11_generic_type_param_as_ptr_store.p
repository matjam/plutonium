package main

import (
	"unsafe"
)

type Holder[P any] struct{ p P }

var gh Holder[*int]

func (h *Holder[P]) put(p P) { h.p = p }

//pu:noinline
func f() {
	x := 42
	h := &gh
	h.put(&x)
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
