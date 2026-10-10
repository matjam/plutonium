package main

type node struct {
	next *node
	val  int
}

func (n *node) second() int {
	return n.next.val
}

func sum(n *node) int {
	return n.val + n.second()
}

func main() {
	n := &node{val: 1}
	println(sum(n))
}
