package main

import "pluto/alloc"

type node struct {
	next *node
	v    int
}

type List owner struct {
	head *node
}

func (l *List) Push(v int) {
	n := alloc.New[node]()
	n.v, n.next = v, l.head
	l.head = n
}

func (l *List) Drop() {
	for l.head != nil {
		n := l.head
		l.head = n.next
		alloc.Free(n)
	}
}

func sum(l List) {
	t := 0
	for n := l.head; n != nil; n = n.next {
		t += n.v
	}
	println(t)
}

var keep *node

// A Lender may keep memory of the list it is given.
type Lender interface {
	Lend(l *List)
}

func stash(p *node) { keep = p }

type stasher struct{}

func (stasher) Lend(l *List) { stash(l.head) }

// lend passes l on to x, whose only implementation keeps its first node.
func lend(l *List, x Lender) { x.Lend(l) }

func main() {
	var l List
	l.Push(1)
	lend(&l, stasher{})
	task sum(l)
	keep.v = 9
}
