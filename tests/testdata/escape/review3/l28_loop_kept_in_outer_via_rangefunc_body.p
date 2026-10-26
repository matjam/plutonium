package main

import (
	"unsafe"
)

func seq(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

//pu:noinline
func f() {
	var keepP *int
	for v := range seq {
		w := v * 10
		keepP = &w
	}
	println("kept:", *keepP)
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
