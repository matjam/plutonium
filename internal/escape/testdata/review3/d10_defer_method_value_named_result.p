package main

import (
	"unsafe"
)

type S struct{ n int }

func (s *S) Get() int { return s.n }

var keepF func() int

//pu:noinline
func g() (r func() int) {
	s := S{42}
	defer func() { r = s.Get }()
	return nil
}

//pu:noinline
func f() { keepF = g() }

func kept() unsafe.Pointer { println(clobber(7)); println("method value returns", keepF()); return nil }

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
