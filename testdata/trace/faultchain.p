package main

// A nil dereference two inlined calls deep, in an unrolled loop: value is
// inlined into walk, and walk into main.

type Node struct {
	next *Node
	val  int
}

func value(n *Node) int {
	return n.val
}

func walk(n *Node) int {
	sum := 0
	for i := 0; i < 3; i++ {
		sum += value(n)
		n = n.next
	}
	return sum
}

//pu:noinline
func main() {
	tail := Node{val: 2}
	head := Node{next: &tail, val: 1}
	println(walk(&head))
}
