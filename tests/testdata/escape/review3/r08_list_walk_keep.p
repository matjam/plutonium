package main

import (
	"unsafe"
)

type Node struct {
	v	int
	next *Node
}

var keep *Node

//pu:noinline
func find(n *Node) {
	for p := n; p != nil; p = p.next {
		if p.v == 42 {
			keep = p
		}
	}
}

//pu:noinline
func f() {
	c := Node{42, nil}
	b := Node{2, &c}
	a := Node{1, &b}
	find(&a)
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
