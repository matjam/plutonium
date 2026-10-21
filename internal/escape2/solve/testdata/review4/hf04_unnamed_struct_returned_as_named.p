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

//pu:noinline
func mk(p *int) Node {
	return struct {
		n	int
		next *int
	}{1, p}
}

//pu:noinline
func f() {
	a := 42
	nd := mk(&a)
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
