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

type Raw struct {
	n	int
	next *int
}

func conv[T ~struct {
	n	int
	next *int
}](r Raw) T {
	return T(r)
}

//pu:noinline
func f() {
	a := 42
	nd := conv[Node](Raw{1, &a})
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
