package main

import "pluto/sync"

var (
	mu sync.Mutex
	m  [8]int
)

func put(i, v int) bool {
	mu.Lock()
	if i < 0 {
		mu.Unlock()
		mu.Unlock()
		return false
	}
	m[i] = v
	mu.Unlock()
	return true
}

func worker(done chan bool) {
	put(1, 2)
	done <- true
}

func main() {
	done := make(chan bool)
	task worker(done)
	put(3, 3)
	<-done
}
