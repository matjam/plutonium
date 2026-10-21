package main

import (
	"unsafe"
	"pluto/vec"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type S struct{ dst *vec.Vec[*int]; src *vec.Vec[*int] }

func link(s *S) { s.dst.Push(s.src.At(0)) }

var gv vec.Vec[*int]

//pu:noinline
func f() {
	x := 42
	local := vec.New[*int]()
	local.Push(&x)
	s := S{&gv, &local}
	link(&s)
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
