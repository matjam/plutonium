package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func add(done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	n++
	done <- true
}

func main() {
	done := make(chan bool)
	task add(done)
	task add(done)
	<-done
	<-done
	mu.Lock()
	defer mu.Unlock()
	println(n)
}
