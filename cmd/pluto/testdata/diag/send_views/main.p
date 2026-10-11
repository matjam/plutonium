package main

import "pluto/alloc"

type Node struct {
	name  string
	count shared[int]
}

func visit(n *Node) { println(n.name) }

func all(ns []Node) { println(len(ns)) }

type Holder struct {
	nodes [2]*Node
}

func hold(h *Holder) { println(h != nil) }

func index(m map[string]*Node) { println(len(m)) }

// Sendability looks through pointers, slices and maps: two tasks that copy
// the count out of one Node would change it without atomic operations.
func main() {
	n := alloc.New[Node]()
	n.count = alloc.Shared[int]()
	task visit(n)

	var ns []Node
	task all(ns)

	var h *Holder
	task hold(h)

	var m map[string]*Node
	task index(m)

	var ch chan *Node
	_ = ch
}
