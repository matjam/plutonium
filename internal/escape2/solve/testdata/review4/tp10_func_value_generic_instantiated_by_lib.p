package main

import (
	"unsafe"
	"pluto/iter"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func store(p *int) { keep = p }

func apply(g func(*int) *int, p *int) *int { return g(p) }

func id(p *int) *int { store(p); return p }

//pu:noinline
func f() {
	x := 42
	for v := range iter.Map(id, one(&x)) {
		_ = v
	}
}

func one(p *int) iter.Seq[*int] {
	return func(yield func(*int) bool) { yield(p) }
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
