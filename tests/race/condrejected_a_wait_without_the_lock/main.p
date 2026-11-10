package main

import "pluto/sync"

var (
	mu sync.Mutex
	c  sync.Cond
)

func wait(done chan bool) {
	c.Wait(&mu) // mu is not held
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task wait(done)
	c.Signal()
	<-done
}
