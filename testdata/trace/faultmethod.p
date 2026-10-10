package main

// A nil dereference in an unrolled loop of a method inlined into main.

type Node struct {
	next *Node
	val  int
}

func (n *Node) Sum3() int {
	sum := 0
	for i := 0; i < 3; i++ {
		sum += n.val
		n = n.next
	}
	return sum
}

func main() {
	tail := Node{val: 2}
	head := Node{next: &tail, val: 1}
	println(head.Sum3())
}
