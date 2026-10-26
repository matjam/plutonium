package main

import (
	"unsafe"
)

func mk(n int) func() int {
	c := n
	return func() int { return c }
}

//pu:noinline
func f() {
	var fs [2]func() int
	for i := 0; i < 2; i++ {
		fs[i] = mk(42 + i)
	}
	println(fs[0](), fs[1]())
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
