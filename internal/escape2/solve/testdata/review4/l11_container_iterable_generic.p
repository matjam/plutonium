package main

import (
	"unsafe"
	"pluto/iter"
	"pluto/container"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type C struct{}

func (c *C) All() iter.Seq2[int, *int] {
	return func(yield func(int, *int) bool) {
		x := 42
		yield(0, &x)
	}
}

func last[S container.Iterable[int, *int]](c S) *int {
	var r *int
	for _, p := range c {
		r = p
	}
	return r
}

//pu:noinline
func f() {
	var c C
	keep = last(&c)
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
