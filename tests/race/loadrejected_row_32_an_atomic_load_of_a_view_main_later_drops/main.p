package main

import "pluto/sync/atomic"

type Node struct{ n int }

var (
	b owned[Node]
	p atomic.Pointer[Node]
)

func reader() {
	if q := p.Load(); q != nil {
		println(q.n)
	}
}

func main() {
	b = new(Node)
	p.Store(&*b)
	task reader()
	b = new(Node)
}
