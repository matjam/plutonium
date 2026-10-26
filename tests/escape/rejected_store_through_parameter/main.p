package main

import "pluto/alloc"
type Node struct{ next *Node }
func link(n *Node) {
	var x Node
	n.next = &x
}
func main() { link(alloc.New[Node]()) }
