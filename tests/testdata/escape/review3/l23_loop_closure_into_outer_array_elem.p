package main

import (
	"unsafe"
)

//pu:noinline
func f() {
	var fs [3]func() int
	for i := 0; i < 3; i++ {
		fs[i] = func() int { return i }
	}
	println(fs[0](), fs[1](), fs[2]())
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
