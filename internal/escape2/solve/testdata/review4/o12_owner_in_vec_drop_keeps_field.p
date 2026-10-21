package main

import (
	"unsafe"
	"pluto/vec"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type O owner struct{ p *int }

func (o *O) Drop() { keep = o.p }

//pu:noinline
func f() {
	x := 42
	v := vec.New[O]()
	v.Push(O{p: &x})
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
