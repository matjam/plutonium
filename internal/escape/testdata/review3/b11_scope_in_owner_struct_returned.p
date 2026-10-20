package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
)

type Group owner struct {
	s sync.Scope
}

var got = make(chan unsafe.Pointer, 1)
var gg Group

//pu:noinline
func f() {
	var g Group
	x := 42
	g.s.Task(func() {
		time.Sleep(20 * time.Millisecond)
		got <- unsafe.Pointer(&x)
	})
	gg = g
}

func kept() unsafe.Pointer { gg.s.Wait(); return <-got }

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
