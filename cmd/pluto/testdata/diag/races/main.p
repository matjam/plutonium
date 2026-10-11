package main

import "pluto/sync"

// hits is written by every handler with no lock: E3502.
var hits int

func handle(done chan bool) {
	hits++
	done <- true
}

// count is guarded by mu at every write, but main reads it without mu:
// E3503.
var (
	mu    sync.Mutex
	count int
)

func add(done chan bool) {
	mu.Lock()
	count++
	mu.Unlock()
	done <- true
}

// A Counter's n is changed by bump with no lock, so a pointer to one
// cannot cross: E3501.
type Counter struct{ n int }

var counter Counter

func bump(c *Counter, done chan bool) {
	c.n++
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 2; i++ {
		task handle(done)
		task add(done)
	}
	task bump(&counter, done)
	for i := 0; i < 5; i++ {
		<-done
	}
	println(count)
}
