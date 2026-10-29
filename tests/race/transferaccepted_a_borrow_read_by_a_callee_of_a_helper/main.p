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

// First lends the first node.
func (l *List) First() *node { return l.head }

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

func bumpAll(l List) {
	for n := l.head; n != nil; n = n.next {
		n.v++
	}
}

var keep *node

func stash(p *node) { keep = p }

func peek(p *node) int { return p.v }

// look reads the first node through peek, which keeps nothing.
func look(l *List) int { return peek(l.head) }

func main() {
	var l List
	l.Push(1)
	println(look(&l))
	task sum(l)
	println(keep == nil)
}
