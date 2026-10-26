package main

import (
	"unsafe"
	"pluto/iter"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func each[T any](v T) iter.Seq[*T] {
	return func(yield func(*T) bool) {
		x := v
		yield(&x)
	}
}

//pu:noinline
func f() {
	for p := range each(42) {
		keep = p
	}
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
