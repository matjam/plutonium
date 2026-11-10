package main

import "pluto/sync"

// The wait loop of spec section 19.3: Wait(&mu) releases mu while it
// waits and holds it again when it returns, so mu is held at every use
// of jobs.
var (
	mu       sync.Mutex
	nonEmpty sync.Cond
	jobs     [4]int
	n        int
)

func push(j int) {
	mu.Lock()
	jobs[n] = j
	n++
	mu.Unlock()
	nonEmpty.Signal() // needs no lock
}

func worker(done chan int) {
	mu.Lock()
	for n == 0 {
		nonEmpty.Wait(&mu)
	}
	n--
	j := jobs[n]
	mu.Unlock()
	done <- j
}

func main() {
	done := make(chan int)
	task worker(done)
	task worker(done)
	push(1)
	push(2)
	println(<-done + <-done)
}
