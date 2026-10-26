package main

import (
	"unsafe"
)

var keepY func(int) bool

func remember(y func(int) bool) { keepY = y }

func seq(yield func(int) bool) { remember(yield) }

//pu:noinline
func f() {
	x := 42
	for v := range seq {
		println(v + x)
	}
}

func kept() unsafe.Pointer { println(clobber(11)); keepY(1); return nil }

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
