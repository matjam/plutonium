package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var gslot *int

type S struct{ pp **int }

func seq(s *S) func(func(int) bool) {
	return func(yield func(int) bool) {
		s.pp = &gslot
		yield(1)
	}
}

//pu:noinline
func f() {
	x := 42
	var s S
	for i := range seq(&s) {
		_ = i
	}
	*s.pp = &x
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
