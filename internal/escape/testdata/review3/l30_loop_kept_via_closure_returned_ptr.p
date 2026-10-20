package main

import (
	"unsafe"
)

//pu:noinline
func f() {
	var outer *int
	for i := 0; i < 3; i++ {
		v := i * 10
		get := func() *int { return &v }
		if i == 0 {
			outer = get()
		}
	}
	println("outer (0 if each iteration had its own v):", *outer)
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
