package main

import (
	"unsafe"
)

func mk(n int) (func() int, *int) {
	c := n
	return func() int { return c }, &c
}

//pu:noinline
func f() {
	var first *int
	for i := 0; i < 2; i++ {
		_, p := mk(42 + i)
		if i == 0 {
			first = p
		}
	}
	println("first (42 if each call had its own region):", *first)
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
