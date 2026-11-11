package main

import "pluto/arch/x86"

type Node struct{ n int }

var (
	b owned[Node]
	p *Node
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	println(p.n)
}

func main() {
	b = new(Node)
	p = &*b
	_ = x86.InterruptGate(timer, 0x08, 0)
	b = new(Node)
}
