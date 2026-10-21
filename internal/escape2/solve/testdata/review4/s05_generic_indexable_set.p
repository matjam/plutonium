package main

import (
	"unsafe"
	"pluto/container"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type G struct{}

func (g *G) Index(i int) *int	   { return nil }
func (g *G) SetIndex(i int, p *int) { keep = p }

func put[M container.Indexable[int, *int]](m M, p *int) { m[0] = p }

var g G

//pu:noinline
func f() {
	x := 42
	put(&g, &x)
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
