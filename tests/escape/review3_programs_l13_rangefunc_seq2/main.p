package main

import (
	"unsafe"
)

var keep *int

func seq(yield func(int, *int) bool) {
	for i := 0; i < 2; i++ {
		if !yield(i, &i) {
			return
		}
	}
}

//pu:noinline
func g() *int {
	var last *int
	for _, p := range seq {
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
