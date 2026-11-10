package main

import "pluto/sync"

// The deferred calls run in reverse: the Wait runs first, with mu held,
// and takes mu again before the deferred Unlock releases it.
var (
	mu    sync.Mutex
	ready sync.Cond
	n     int
)

func wait(done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	defer ready.Wait(&mu)
	n++
	done <- true
}

func main() {
	done := make(chan bool)
	task wait(done)
	<-done
	mu.Lock()
	n++
	mu.Unlock()
	ready.Broadcast()
}
