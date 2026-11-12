package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Node struct {
	mu sync.Mutex
	v  int
}

var (
	mu sync.Mutex
	m  map[int]synced[Node]
)

func reader(done chan bool) {
	mu.Lock()
	n, ok := m[1]
	mu.Unlock()
	if ok {
		n.mu.Lock()
		n.v++
		n.mu.Unlock()
	}
	done <- true
}

func deleter(done chan bool) {
	mu.Lock()
	delete(m, 1)
	mu.Unlock()
	done <- true
}

func main() {
	m = map[int]synced[Node]{}
	m[1] = alloc.Synced[Node]()
	done := make(chan bool)
	task reader(done)
	task deleter(done)
	<-done
	<-done
}
