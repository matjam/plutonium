package main

import (
	"unsafe"
)

type H struct{ p *int }

var keep *int

func seq(yield func(H) bool) {
	x := 42
	yield(H{&x})
}

//pu:noinline
func g() *int {
	var last H
	for h := range seq {
		last = h
	}
	return last.p
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
