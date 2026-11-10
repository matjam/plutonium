package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func work(done chan bool) {
	u := mu.Unlock
	mu.Lock()
	n++
	u()
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
