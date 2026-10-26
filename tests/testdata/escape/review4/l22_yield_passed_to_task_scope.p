package main

import (
	"unsafe"
	"pluto/sync"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func seq(yield func(*int) bool) {
	x := 42
	var s sync.Scope
	s.Task(func() { yield(&x) })
	s.Wait()
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
