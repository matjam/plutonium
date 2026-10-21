package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type S interface{ SetIndex(i int, p *int) }
type G struct{ p *int }

func (g *G) SetIndex(i int, p *int) { g.p = p }

type W struct{ S }

var gg G
var gw = W{&gg}

//pu:noinline
func f() {
	x := 42
	gw[0] = &x
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
