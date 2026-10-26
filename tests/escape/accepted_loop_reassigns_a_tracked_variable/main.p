package main

import "pluto/alloc"
type Node struct{ next *Node }
var G *Node
func main() {
	var local Node
	p := &local
	for i := 0; i < 3; i++ {
		_ = p.next
		p = alloc.New[Node]()
	}
	q := alloc.New[Node]()
	for i := 0; i < 2; i++ {
		if i == 1 {
			break
		}
		q = alloc.New[Node]()
	}
	G = q
}
