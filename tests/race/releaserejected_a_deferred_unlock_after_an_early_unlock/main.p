package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func work(done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	n++
	mu.Unlock() // the deferred call releases mu again at the end
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	<-done
}
