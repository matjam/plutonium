package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type T struct {
	n int
	m int
}

var keep any

func (t T) String() string { return "t" }

//pu:noinline
func g(a any) {
	s := a.(Stringer)
	_ = s.String()
	keep = s
}

//pu:noinline
func f() {
	t := T{42, 1}
	g(&t)
}

func kept() unsafe.Pointer {
	if p, ok := keep.(*T); ok {
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
