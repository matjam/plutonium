package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func step() {}

// The literal runs before the deferred Unlock, and mu is held at its defer
// statement and at every point after it.
func f(done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	defer func() { n++ }()
	step()
	done <- true
}

func g(done chan bool) {
	mu.Lock()
	n++
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task f(done)
	task g(done)
	<-done
	<-done
}
