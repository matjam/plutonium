package main

import "pluto/sync"

type node struct {
	mu   sync.Mutex
	v    int
	next *node
}

type list struct {
	head *node
}

func (l *list) sum() int {
	prev := l.head
	prev.mu.Lock()
	s := prev.v
	for prev.next != nil {
		cur := prev.next
		cur.mu.Lock()
		prev.mu.Unlock()
		prev = cur
		s += prev.v
	}
	prev.mu.Unlock()
	return s
}

func walk(l *list, done chan bool) {
	println(l.sum())
	done <- true
}

var (
	n2 = node{v: 2}
	n1 = node{v: 1}
	the list
)

func main() {
	n1.next = &n2
	the.head = &n1
	l := &the
	done := make(chan bool)
	task walk(l, done)
	task walk(l, done)
	<-done
	<-done
}
