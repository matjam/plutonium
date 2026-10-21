package main

// A field is heap-only when everything stored in it is heap memory (spec
// section 18.3). A conversion between struct types with the same fields and
// a union variant's composite literal store into the fields too, so a
// pointer into the frame stored that way keeps the field from being
// heap-only, and a function that keeps what the field holds keeps it.

type Node struct {
	v    int
	next *Node
}

type Raw struct {
	v    int
	next *Node
}

type U union {
	None
	N Node
}

var keep *Node

func keepNext(n *Node) { keep = n.next }

// Converted stores &a in b.next through Raw.
func Converted() {
	a := Node{v: 1}
	b := Node(Raw{2, &a})
	keepNext(&b)
}

// Variant stores &a in the payload of a union variant.
func Variant() {
	a := Node{v: 1}
	u := U.N{v: 2, next: &a}
	if b, ok := u.(U.N); ok {
		keepNext(&b)
	}
}

func main() {
	Converted()
	Variant()
}
