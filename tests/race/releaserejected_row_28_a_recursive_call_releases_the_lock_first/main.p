package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func f(k int) {
	if k > 0 {
		f(k - 1)
		mu.Unlock()
	}
}

func work(done chan bool) {
	mu.Lock()
	n++
	f(1)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	<-done
}
