package main

type Node struct {
	v    int
	next *Node
}
type Raw struct {
	v    int
	next *Node
}
var keep *Node
func keepNext(n *Node) { keep = n.next }
func f() {
	a := Node{42, nil}
	b := Node(Raw{1, &a})
	keepNext(&b)
}
func main() { f() }
