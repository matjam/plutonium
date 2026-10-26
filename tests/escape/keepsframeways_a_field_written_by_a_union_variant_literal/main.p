package main

type Node struct {
	v    int
	next *Node
}
type U union {
	None
	N Node
}
var keep *Node
func keepNext(n *Node) { keep = n.next }
func f() {
	a := Node{42, nil}
	u := U.N{v: 1, next: &a}
	if b, ok := u.(U.N); ok {
		keepNext(&b)
	}
}
func main() { f() }
