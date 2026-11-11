package main

import (
	"pluto/sync"
	"pluto/vec"
)

var (
	rw    sync.RWMutex
	items vec.Vec[int]
)

func grow(done chan bool) {
	rw.Lock()
	items.Push(1)
	rw.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	items.Push(0)
	task grow(done)
	rw.RLock()
	p := items.Ptr(0)
	rw.RUnlock()
	rw.Lock()
	_ = *p
	rw.Unlock()
	<-done
}
