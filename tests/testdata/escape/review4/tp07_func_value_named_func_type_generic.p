package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func store(p *int) { keep = p }

type Fn[T any] func(*T)

func call[T any](g Fn[T], p *T) { g(p) }

//pu:noinline
func f() {
	x := 42
	call(Fn[int](store), &x)
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
