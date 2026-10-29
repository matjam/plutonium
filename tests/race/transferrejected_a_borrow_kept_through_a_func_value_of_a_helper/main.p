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

func stash(p *node) { keep = p }

// lend hands the first node to f, which may be stash.
func lend(l *List, f func(*node)) { f(l.head) }

func main() {
	var l List
	l.Push(1)
	lend(&l, stash)
	task sum(l)
	keep.v = 9
}
