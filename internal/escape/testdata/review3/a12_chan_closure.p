package main

import (
	"unsafe"
)

var ch = make(chan func() int, 1)

//pu:noinline
func f() {
	x := 42
	ch <- func() int { return x }
}

func kept() unsafe.Pointer {
	g := <-ch
	println("closure returns", g())
	return nil
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
