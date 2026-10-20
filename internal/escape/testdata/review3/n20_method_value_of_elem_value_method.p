package main

import (
	"unsafe"
)

type T struct {
	p *int
	n int
}

var keep *int

func (t T) Get() int { keep = t.p; return 1 }

//pu:noinline
func callf(f func() int) int { return f() }

//pu:noinline
func g(p *[2]T) int { return callf(p[1].Get) }

//pu:noinline
func f() {
	x := 42
	var arr [2]T
	arr[1] = T{&x, 1}
	println(g(&arr))
}

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

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
