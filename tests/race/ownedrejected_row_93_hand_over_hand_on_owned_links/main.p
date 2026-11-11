package main

import "pluto/sync"

type Node owner struct {
	mu   sync.Mutex
	v    int
	next owned[Node]
}

var head owned[Node]

func walk(done chan bool) {
	n := &*head
	n.mu.Lock()
	for n.next != nil {
		next := &*n.next
		next.mu.Lock()
		n.v++
		n.mu.Unlock()
		n = next
	}
	n.v++
	n.mu.Unlock()
	done <- true
}

func main() {
	head = new(Node{v: 1})
	head.next = new(Node{v: 2})
	head.next.next = new(Node{v: 3})
	done := make(chan bool)
	task walk(done)
	task walk(done)
	<-done
	<-done
}
