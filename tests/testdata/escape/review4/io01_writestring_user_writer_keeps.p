package main

import (
	"unsafe"
	"pluto/io"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type W struct{}

var keepB []byte

func (W) Write(p []byte) (int, error) { keepB = p; return len(p), nil }

//pu:noinline
func f() {
	b := [2]byte{42, 43}
	var w W
	w.Write(b[:])
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
