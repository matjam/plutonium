package main

import (
	"unsafe"
)

type Setter[T any] interface{ Set(v T) }
type Box[T any] struct{ v T }

func (b *Box[T]) Set(v T) { b.v = v }

var gb Box[*int]

func put[T any, S Setter[T]](s S, v T) { s.Set(v) }

//pu:noinline
func f() {
	x := 42
	put[*int](&gb, &x)
}

func kept() unsafe.Pointer { return unsafe.Pointer(gb.v) }

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
