package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type Inner struct{ n int }
type Outer struct{ *Inner }

var keep *Inner

func (i *Inner) String() string { keep = i; return "i" }

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func f() {
	in := Inner{42}
	println(call(Outer{&in}))
}

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var sink Stringer = &Inner{}

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
