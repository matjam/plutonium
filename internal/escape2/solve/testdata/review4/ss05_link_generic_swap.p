package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func swap[T any](a, b *T) { *a, *b = *b, *a }

type S struct{ r **int; p **int }

func link(s *S) { swap(s.r, s.p) }

//pu:noinline
func f() {
	x := 42
	var out *int
	px := &x
	s := S{&out, &px}
	link(&s)
	keep = out
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
