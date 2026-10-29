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

// A Reader is given a node to look at.
type Reader interface {
	Read(p *node) int
}

type plain struct{}

func (plain) Read(p *node) int { return p.v }

type doubled struct{ k int }

func (d *doubled) Read(p *node) int { return d.k * p.v }

// keeper keeps the node, but is never converted to a Reader, so no call
// through one reaches it.
type keeper struct{}

func (keeper) Read(p *node) int {
	keep = p
	return 0
}

// look reads the first node through r: every Reader the program makes
// only reads it.
func look(l *List, r Reader) int { return r.Read(l.head) }

func main() {
	var l List
	l.Push(1)
	d := doubled{k: 2}
	println(look(&l, plain{}) + look(&l, &d))
	task sum(l)
	var k keeper
	println(k.Read(nil), keep == nil)
}
