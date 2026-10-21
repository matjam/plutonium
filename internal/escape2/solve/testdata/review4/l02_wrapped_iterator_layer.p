package main

import (
	"unsafe"
	"pluto/iter"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func seq(yield func(*int) bool) {
	x := 42
	yield(&x)
}

func wrap(s iter.Seq[*int]) iter.Seq[*int] {
	return func(yield func(*int) bool) {
		for p := range s {
			if !yield(p) {
				return
			}
		}
	}
}

//pu:noinline
func f() {
	for p := range wrap(seq) {
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
