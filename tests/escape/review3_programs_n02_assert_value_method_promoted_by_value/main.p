package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type T struct {
	p *int
	n int
}
type U struct {
	a int
	T
}

var keep *int

func (t T) String() string { keep = t.p; return "t" }

//pu:noinline
func g(a any) string { return a.(Stringer).String() }

//pu:noinline
func f() {
	x := 42
	u := U{1, T{&x, 1}}
	println(g(&u))
}

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var sink Stringer = T{}

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
