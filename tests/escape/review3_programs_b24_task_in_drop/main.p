package main

import (
	"unsafe"
	"pluto/time"
)

type O owner struct{ p *int }

var got = make(chan unsafe.Pointer, 1)

func use(p *int) {
	time.Sleep(10 * time.Millisecond)
	got <- unsafe.Pointer(p)
}

func (o *O) Drop() { task use(o.p) }

//pu:noinline
func f() {
	x := 42
	o := O{&x}
	_ = o.p
}

func kept() unsafe.Pointer { return <-got }

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
