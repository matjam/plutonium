package main

import (
	"unsafe"
)

type Sender interface{ Send(p *int) }
type cs struct{ c chan *int }

func (s cs) Send(p *int) { s.c <- p }

var ch = make(chan *int, 1)

//pu:noinline
func use(s Sender, p *int) { s.Send(p) }

//pu:noinline
func f() {
	x := 42
	use(cs{ch}, &x)
}

func kept() unsafe.Pointer { return unsafe.Pointer(<-ch) }

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
