package main

import (
	"unsafe"
	"pluto/vec"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type S struct{ r *vec.Vec[*int]; p *int }

func (s *S) push() { s.r.Push(s.p) }

var gv vec.Vec[*int]

//pu:noinline
func f() {
	x := 42
	s := S{&gv, &x}
	s.push()
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
