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

func (l *list) first() int {
	l.head.mu.Lock()
	v := l.head.v
	l.head.mu.Unlock() // l.head is loaded again: it may point to another node now
	return v
}

func walk(l *list, done chan bool) {
	println(l.first())
	done <- true
}

var (
	n1  = node{v: 1}
	the list
)

func main() {
	the.head = &n1
	l := &the
	done := make(chan bool)
	task walk(l, done)
	task walk(l, done)
	<-done
	<-done
}
