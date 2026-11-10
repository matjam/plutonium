package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func swap(p, q *sync.Mutex) {
	p.Lock()
	n++
	q.Unlock() // q may be another mutex: the caller must hold it
}

func work(done chan bool) {
	swap(&mu, &mu)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	<-done
}
