package main

type Node struct{ n int }

var (
	b owned[Node]
	p *Node
)

func reader() { println(p.n) }

func main() {
	b = new(Node)
	p = &*b
	task reader()
	b = new(Node)
}
