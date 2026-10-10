package main

// A nil dereference in a loop that is inlined into main and, at -O2, fully
// unrolled: the copies of the loop body keep their inlined call.

type Node struct {
	next *Node
	val  int
}

func walk(n *Node) int {
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
	println(walk(&head))
}
