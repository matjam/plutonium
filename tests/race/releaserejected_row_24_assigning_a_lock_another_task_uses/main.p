package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func use(done chan bool) {
	mu.Lock()
	n++
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task use(done)
	mu = sync.Mutex{}
	<-done
}
