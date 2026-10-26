package main

type Node struct{ next *Node }
func main() {
	var x Node
	n := new(Node)
	n.next = &x
}
