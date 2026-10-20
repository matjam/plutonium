package main

import (
	"unsafe"
)

type Keeper interface{ Keep() }
type T struct {
	n int
	m int
}

var keep *T

func (t *T) Keep() { keep = t }

func each[K Keeper](ks []K) {
	for _, k := range ks {
		k.Keep()
	}
}

//pu:noinline
func f() {
	t := T{42, 1}
	arr := [1]*T{&t}
	each(arr[:])
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
