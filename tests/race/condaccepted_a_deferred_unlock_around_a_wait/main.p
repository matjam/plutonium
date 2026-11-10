package main

import "pluto/sync"

// A Wait after defer mu.Unlock() keeps mu held for the deferred call:
// Wait takes the lock again before it returns.
var (
	mu    sync.Mutex
	ready sync.Cond
	ok    bool
)

func wait(done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	for !ok {
		ready.Wait(&mu)
	}
	done <- ok
}

func main() {
	done := make(chan bool)
	task wait(done)
	mu.Lock()
	ok = true
	mu.Unlock()
	ready.Broadcast()
	println(<-done)
}
