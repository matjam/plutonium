package main

import "pluto/sync"

var (
	rw sync.RWMutex
	n  int
)

func read() int {
	rw.RLock()
	v := n
	rw.RUnlock()
	return v
}

func write(done chan bool) {
	rw.Lock()
	n++
	rw.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task write(done)
	println(read())
	<-done
}
