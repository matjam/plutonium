package main

import (
	"unsafe"
)

var gstr string

//pu:noinline
func f() {
	buf := [8]byte{'*', 0, 0, 0, 0, 0, 0, 0}
	gstr = unsafe.String(&buf[0], 8)
}

func kept() unsafe.Pointer { return unsafe.Pointer(unsafe.StringData(gstr)) }

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
