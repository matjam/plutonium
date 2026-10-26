package main

import (
	"unsafe"
)

type G struct{ p *int }

func (g *G) SetIndex(i int, p *int) { g.p = p }
func (g *G) Index(i int) *int	{ return g.p }
func (g *G) Len() int			{ return 1 }

var keep *int

//pu:noinline
func f() {
	x := 42
	var g G
	g.p = &x
	keep = g[0]
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
