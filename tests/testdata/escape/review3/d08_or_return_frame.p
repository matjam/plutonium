package main

import (
	"unsafe"
)

type E struct{ n int }

func (e *E) Error() string { return "e" }

var keep error

//pu:noinline
func h() (int, error) { return 0, nil }

//pu:noinline
func g() error {
	e := E{42}
	var err error = &e
	_ = h() or return err
	return err
}

//pu:noinline
func f() { keep = g() }

func kept() unsafe.Pointer {
	if p, ok := keep.(*E); ok {
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
