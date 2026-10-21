package main

import (
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

type Node struct {
	n	int
	next *int
}

func keepNext(n *Node) { keep = n.next }

func id[T any](v T) T { return v }

//pu:noinline
func f() {
	a := 42
	anon := struct {
		n	int
		next *int
	}{1, &a}
	var nd Node = id(anon)
	keepNext(&nd)
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
