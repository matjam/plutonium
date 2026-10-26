package main

import (
	"unsafe"
)

type S struct{ n int }

var keep *S

func (s *S) String() string {
	keep = s
	return "s"
}

type Stringer interface{ String() string }

type Box[T any] struct {
	v T
	k int
}

func (b Box[T]) String() string {
	if s, ok := any(b.v).(Stringer); ok {
		return s.String()
	}
	return "?"
}

func show[T any](v T) string {
	var st Stringer = Box[T]{v, 1}
	return st.String()
}

//pu:noinline
func f() {
	s := S{42}
	println(show(&s))
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
