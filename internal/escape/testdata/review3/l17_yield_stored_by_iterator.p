package main

import (
	"unsafe"
)

var keepY func(*int) bool

func seq(yield func(*int) bool) {
	keepY = yield
}

//pu:noinline
func f() {
	x := 42
	for p := range seq {
		_ = p
		_ = x
	}
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
