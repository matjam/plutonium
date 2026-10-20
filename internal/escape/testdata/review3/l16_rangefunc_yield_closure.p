package main

import (
	"unsafe"
)

var keepF func() int

func seq(yield func(func() int) bool) {
	x := 42
	yield(func() int { return x })
}

//pu:noinline
func g() func() int {
	var last func() int
	for c := range seq {
		last = c
	}
	return last
}

//pu:noinline
func f() { keepF = g() }

func kept() unsafe.Pointer { println(clobber(3)); println("closure returns", keepF()); return nil }

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
