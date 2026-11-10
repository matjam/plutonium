package main

import "pluto/sync"

var (
	rw    sync.RWMutex
	c     sync.Cond
	ready bool
)

func wait(done chan bool) {
	rw.RLock()
	for !ready {
		c.Wait(&rw) // Wait needs rw held for writing
	}
	rw.RUnlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task wait(done)
	rw.Lock()
	ready = true
	rw.Unlock()
	c.Broadcast()
	<-done
}
