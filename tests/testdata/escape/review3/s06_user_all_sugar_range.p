package main

import (
	"unsafe"
	"pluto/iter"
)

type C struct{ n int }

func (c *C) All() iter.Seq2[int, *int] {
	return func(yield func(int, *int) bool) {
		local := c.n
		yield(0, &local)
	}
}

var keep *int

//pu:noinline
func g() *int {
	c := C{42}
	var last *int
	for _, p := range c {
		last = p
	}
	return last
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
