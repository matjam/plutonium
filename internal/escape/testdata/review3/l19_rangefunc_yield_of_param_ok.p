package main

import (
	"unsafe"
)

var keep *int
var gx = 42

func seq(yield func(*int) bool) {
	yield(&gx)
}

//pu:noinline
func f() {
	var last *int
	for p := range seq {
		last = p
	}
	keep = last
}

func kept() unsafe.Pointer { println("control: static memory"); return nil }

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
