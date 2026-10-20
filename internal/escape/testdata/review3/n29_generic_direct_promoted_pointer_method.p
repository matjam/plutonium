package main

import (
	"unsafe"
)

type Stringer interface{ String() string }
type T0 struct {
	p *int
	n int
}
type U struct {
	k int
	T0
}

var keep *T0

func (t *T0) String() string { keep = t; return "t0" }

func show[T any](v *T) string {
	if s, ok := any(v).(Stringer); ok {
		return s.String()
	}
	return ""
}

//pu:noinline
func f() {
	u := U{1, T0{nil, 42}}
	println(show(&u))
}

func kept() unsafe.Pointer { return unsafe.Pointer(&keep.n) }

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
