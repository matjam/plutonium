package main

import (
	"unsafe"
)

type Keeper interface{ Keep(p *int) }
type K struct{}

var keep *int

func (K) Keep(p *int) { keep = p }

var hook func(*int)

//pu:noinline
func f() {
	x := 42
	hook(&x)
}

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

func init() {
	var k Keeper = K{}
	hook = k.Keep
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
