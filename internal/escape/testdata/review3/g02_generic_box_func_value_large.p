package main

import (
	"unsafe"
)

type Big [4]int

func box[T any](v T) any { return any(v) }

var keep any

//pu:noinline
func f() {
	fv := box[Big]
	keep = fv(Big{42, 1, 2, 3})
}

func kept() unsafe.Pointer {
	println(clobber(4))
	b := keep.(Big)
	println("value (42 if intact):", b[0])
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
