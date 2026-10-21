package main

import (
	"unsafe"
	"pluto/fmt"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type S struct{ n int }

func (s *S) String() string { keep = &s.n; return "s" }

func mk[T any](v *T) func() string {
	return func() string { return any(v).(fmt.Stringer).String() }
}

//pu:noinline
func f() {
	s := S{42}
	_ = mk(&s)()
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
