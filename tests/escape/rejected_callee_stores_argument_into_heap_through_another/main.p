package main

import "pluto/alloc"
type Node struct{ next *Node }
func link(a, b *Node) { a.next = b }
func main() {
	var x Node
	link(alloc.New[Node](), &x)
}
