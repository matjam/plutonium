package main

import "pluto/sync"

type S struct {
	mu sync.Mutex
	ch chan int
}

var b owned[S]

func sender(done chan bool) {
	b.mu.Lock()
	b.ch <- 1
	b.mu.Unlock()
	done <- true
}

func main() {
	b = new(S{ch: make(chan int, 4)})
	done := make(chan bool)
	task sender(done)
	task sender(done)
	<-done
	<-done
	println(<-b.ch + <-b.ch)
}
