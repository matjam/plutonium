package main

import "pluto/sync"

var (
	rw sync.RWMutex
	c  sync.Cond
)

func wait(done chan bool) {
	rw.RLock()
	c.Wait(rw.RLocker()) // a Locker value names no lock
	rw.RUnlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task wait(done)
	c.Signal()
	<-done
}
