package main

import "pluto/sync"

type S struct {
	mu sync.Mutex
	n  int
}

var b owned[S]

func worker(done chan bool) {
	b.mu.Lock()
	b.n++
	b.mu.Unlock()
	done <- true
}

func main() {
	b = new(S{})
	done := make(chan bool)
	task worker(done)
	task worker(done)
	<-done
	<-done
	b.mu.Lock()
	println(b.n)
	b.mu.Unlock()
}
