package main

import (
	"unsafe"
)

type U union {
	None
	P *int
}
type Maker interface{ Make(p *int) U }
type M struct{ mk func(*int) U }

func (m M) Make(p *int) U { return m.mk(p) }

var keep U

//pu:noinline
func g(m Maker) U {
	x := 42
	return m.Make(&x)
}

//pu:noinline
func f() { keep = g(M{U.P}) }

func kept() unsafe.Pointer {
	if p, ok := keep.(U.P); ok {
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
