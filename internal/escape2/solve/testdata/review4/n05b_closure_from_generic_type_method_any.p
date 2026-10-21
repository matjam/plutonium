package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var keepAny any

type F[T any] struct{}

func mk[T any]() func(*T) { return func(p *T) { keepAny = p } }

func (F[T]) Make() func(*T) { return mk[T]() }

var fv F[int]

//pu:noinline
func f() {
	x := 42
	g := fv.Make()
	g(&x)
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
