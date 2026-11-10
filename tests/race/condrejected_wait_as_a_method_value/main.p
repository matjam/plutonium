package main

import "pluto/sync"

var (
	mu sync.Mutex
	c  sync.Cond
)

func wait(done chan bool) {
	w := c.Wait // called through a func value, Wait counts as called with no lock held
	mu.Lock()
	w(&mu)
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task wait(done)
	c.Signal()
	<-done
}
