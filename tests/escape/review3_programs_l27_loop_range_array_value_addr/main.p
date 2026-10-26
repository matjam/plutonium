package main

import (
	"unsafe"
)

//pu:noinline
func f() {
	arr := [3]int{1, 2, 3}
	var ps [3]*int
	for i, v := range arr {
		ps[i] = &v
	}
	println(*ps[0], *ps[1], *ps[2])
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
