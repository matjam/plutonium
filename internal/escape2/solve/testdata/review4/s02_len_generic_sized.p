package main

import (
	"unsafe"
	"pluto/container"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type C struct{ n int }

func (c *C) Len() int { keep = &c.n; return 1 }

func size[S container.Sized](s S) int { return len(s) }

//pu:noinline
func f() {
	var c C
	c.n = 42
	_ = size(&c)
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
