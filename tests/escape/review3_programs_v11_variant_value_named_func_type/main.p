package main

import (
	"unsafe"
)

type U union {
	None
	P *int
}
type Mk func(*int) U

var keep U

//pu:noinline
func g() U {
	x := 42
	var m Mk = U.P
	return m(&x)
}

//pu:noinline
func f() { keep = g() }

func kept() unsafe.Pointer {
	if p, ok := keep.(U.P); ok {
		return unsafe.Pointer(p)
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
