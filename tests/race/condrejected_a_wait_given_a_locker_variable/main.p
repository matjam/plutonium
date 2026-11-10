package main

import "pluto/sync"

var (
	mu sync.Mutex
	c  sync.Cond
)

func wait(done chan bool) {
	var l sync.Locker = &mu
	mu.Lock()
	c.Wait(l) // a Locker value names no lock
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task wait(done)
	c.Signal()
	<-done
}
