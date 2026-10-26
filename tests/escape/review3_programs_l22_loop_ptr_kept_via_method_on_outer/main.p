package main

import (
	"unsafe"
)

type Holder struct{ p *int }

func (h *Holder) set(p *int) { h.p = p }

//pu:noinline
func f() {
	var h Holder
	for i := 0; i < 3; i++ {
		v := i * 10
		h.set(&v)
	}
	println("h.p (20 if each iteration had its own v):", *h.p)
}

func kept() unsafe.Pointer { return nil }

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
