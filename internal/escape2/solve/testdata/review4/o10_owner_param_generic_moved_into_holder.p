package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type O owner struct{ p *int }

func (o *O) Drop() { keep = o.p }

type H[T owner any] owner struct{ t T }

func hold[T owner any](t T) {
	var h H[T]
	h.t = t
}

//pu:noinline
func f() {
	x := 42
	hold(O{p: &x})
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
