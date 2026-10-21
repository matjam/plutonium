package main

import (
	"unsafe"
)

type R owner interface{ Read() int }
type O owner struct{ n int }

var keep *O

func (o *O) Read() int { return o.n }
func (o *O) Drop() { keep = o }

//pu:noinline
func f() {
	var o O
	o.n = 42
	o.Read()
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
