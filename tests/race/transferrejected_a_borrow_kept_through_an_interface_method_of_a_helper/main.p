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

// A Stasher may keep the node it is given.
type Stasher interface {
	Stash(p *node)
}

type global struct{}

func (global) Stash(p *node) { keep = p }

// lend hands the first node to s, whose only implementation keeps it.
func lend(l *List, s Stasher) { s.Stash(l.head) }

func main() {
	var l List
	l.Push(1)
	lend(&l, global{})
	task sum(l)
	keep.v = 9
}
