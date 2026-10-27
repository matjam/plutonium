package main
import "pluto/alloc"

type Node struct{ count shared[int] }

func keep(a any) {}

func lend(n *Node) {
	keep(n)
}

func take(c chan any) { <-c }

func main() {
	n := &theNode
	n.count = alloc.Shared[int]()
	lend(n)
	c := make(chan any, 1)
	task take(c)
	c <- n
}