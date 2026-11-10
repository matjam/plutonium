package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func step() {}

// n++ runs with no lock held if step ends the task before mu.Lock, and in
// any case after the deferred Unlock.
func f(done chan bool) {
	defer func() { n++ }()
	mu.Lock()
	defer mu.Unlock()
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
