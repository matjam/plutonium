package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func run(ys []func(*int) bool) {
	x := 42
	ys[0](&x)
}

func seq(yield func(*int) bool) {
	ys := [1]func(*int) bool{yield}
	run(ys[:])
}

//pu:noinline
func f() {
	for p := range seq {
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
