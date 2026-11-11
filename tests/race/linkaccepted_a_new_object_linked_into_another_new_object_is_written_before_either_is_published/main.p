package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type node struct {
	val  int
	next *node
}

var (
	mu    sync.Mutex
	probe *node
)

func reader(done chan bool) {
	mu.Lock()
	if probe != nil {
		println(probe.next.val)
	}
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task reader(done)
	a := alloc.New[node]()
	a.val = 7
	a.next = alloc.New[node]()
	a.next.val = 42
	mu.Lock()
	probe = a
	mu.Unlock()
	<-done
}
