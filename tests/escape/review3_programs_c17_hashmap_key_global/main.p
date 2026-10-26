package main

import (
	"unsafe"
	"pluto/hash"
)

var gm hash.Map[*int, int]

//pu:noinline
func f() {
	x := 42
	gm.Set(&x, 1)
}

func kept() unsafe.Pointer {
	for k, _ := range gm.All() {
		return unsafe.Pointer(k)
	}
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
