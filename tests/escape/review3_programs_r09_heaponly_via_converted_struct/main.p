package main

import (
	"unsafe"
)

type Node struct {
	v	int
	next *Node
}
type Raw struct {
	v	int
	next *Node
}

var keep *Node

//pu:noinline
func keepNext(n *Node) { keep = n.next }

//pu:noinline
func f() {
	a := Node{42, nil}
	b := Node(Raw{1, &a})
	keepNext(&b)
}

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

var heapNode = Node{7, nil}

func init() {
	var h Node
	h.next = &heapNode
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
