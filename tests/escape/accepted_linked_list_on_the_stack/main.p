package main

type Node struct {
	next *Node
	v    int
}
func sum(n *Node) int {
	s := 0
	for ; n != nil; n = n.next {
		s += n.v
	}
	return s
}
func main() {
	var a, b, c Node
	a.next = &b
	b.next = &c
	_ = sum(&a)
}
