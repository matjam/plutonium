package main

import (
	"unsafe"
)

type K struct{ p *int }

func (k *K) Keep(p *int) { k.p = p }

var gk K
var hook func(*K, *int) = (*K).Keep

//pu:noinline
func f() {
	x := 42
	hook(&gk, &x)
}

func kept() unsafe.Pointer { return unsafe.Pointer(gk.p) }

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
