package main

import (
	"unsafe"
)

type Big struct{ a, b, c int }

//pu:noinline
func f() {
	var keepI any
	for i := 0; i < 3; i++ {
		keepI = Big{i, i, i}
	}
	println("kept:", keepI.(Big).a)
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
