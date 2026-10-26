package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type S struct{ n int }
type H struct {
	a int
	s S
}

var keep *S

func (s *S) String() string { keep = s; return "s" }

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func g(p *[2]H) string { return call(&p[1].s) }

//pu:noinline
func f() {
	var arr [2]H
	arr[1].s.n = 42
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
