package main

import (
	"unsafe"
)

var keep *int

//pu:noinline
func f() {
	var ps [3]*int
	n := 0
top:
	v := n
	ps[n] = &v
	n++
	if n < 3 {
		goto top
	}
	println("distinct per iteration:", ps[0] != ps[1], *ps[0], *ps[1], *ps[2])
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
