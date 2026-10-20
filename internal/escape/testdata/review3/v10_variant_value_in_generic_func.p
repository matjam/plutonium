package main

import (
	"unsafe"
)

type Opt[T any] union {
	None
	Some T
}

func wrap[T any](v T) Opt[T] {
	mk := Opt[T].Some
	return mk(v)
}

var keep Opt[*int]

//pu:noinline
func g() Opt[*int] {
	x := 42
	return wrap(&x)
}

//pu:noinline
func f() { keep = g() }

func kept() unsafe.Pointer {
	if p, ok := keep.(Opt[*int].Some); ok {
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
