package main

import (
	"unsafe"
)

type Maker interface{ Make(n int) func() int }
type M struct{}

func (M) Make(n int) func() int { return func() int { return n } }

var keepF func() int

//pu:noinline
func g(m Maker) func() int { return m.Make(42) }

//pu:noinline
func f() { keepF = g(M{}) }

func kept() unsafe.Pointer { println(clobber(2)); println("closure returns (42 if intact)", keepF()); return nil }

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
