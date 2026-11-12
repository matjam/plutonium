package main

import (
	"pluto/alloc"
	"pluto/sync"
)

// The twin of accepted_row_68_a_locked_mutex_moves_with_its_owner: Lend
// locks the mutex as Lock does, and also hands the first node to stash,
// which keeps it, so the send leaves an alias of what b holds behind.

type node struct {
	next *node
	v    int
}

type Box owner struct {
	mu   sync.Mutex
	head *node
	n    int
}

func (b *Box) Push(v int) {
	p := alloc.New[node]()
	p.v, p.next = v, b.head
	b.head = p
}

func (b *Box) Drop() {
	for b.head != nil {
		p := b.head
		b.head = p.next
		alloc.Free(p)
	}
}

var keep *node

func stash(p *node) { keep = p }

func (b *Box) Lend() {
	b.mu.Lock()
	stash(b.head)
}

func worker(ch chan Box, done chan int) {
	b := <-ch
	done <- b.n
}

func main() {
	var b Box
	b.Push(1)
	b.Lend()
	b.n = 2
	ch := make(chan Box, 1)
	ch <- b
	done := make(chan int)
	task worker(ch, done)
	println(<-done)
	keep.v = 9
}
