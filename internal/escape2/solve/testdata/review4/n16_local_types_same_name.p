package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func g1() any {
	type L struct{ n int }
	return L{}
}

type Putter interface{ Put(p *int) }

func g2() Putter {
	return T2{}
}

type T2 struct{}

func (T2) Put(p *int) { keep = p }

//pu:noinline
func f() {
	x := 42
	_ = g1()
	g2().Put(&x)
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
