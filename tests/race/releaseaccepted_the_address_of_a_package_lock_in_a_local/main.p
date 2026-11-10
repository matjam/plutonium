package main

import "pluto/sync"

// Audit L13: p := &mu makes *p a name of mu.
var (
	mu sync.Mutex
	x  int
)

func work(done chan bool) {
	p := &mu
	p.Lock()
	x++
	p.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	mu.Lock()
	x++
	mu.Unlock()
	<-done
}
