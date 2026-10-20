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

func first[T any](v []T) string {
	var a any = v
	if xs, ok := a.([]*S); ok {
		var st Stringer = xs[0]
		return st.String()
	}
	return "?"
}

//pu:noinline
func f() {
	s := S{42}
	arr := [1]*S{&s}
	println(first(arr[:]))
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
