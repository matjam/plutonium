package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type S struct{ n int }
type Box struct{ arr *[2]S }

var keep *S

func (s *S) String() string { keep = s; return "s" }

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func g(b *Box) string { return call(&b.arr[1]) }

//pu:noinline
func f() {
	var arr [2]S
	arr[1].n = 42
	b := Box{&arr}
	println(g(&b))
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
