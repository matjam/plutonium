package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func step() {}

// At the exit mu is held, but if step ends the task the literal may run at
// any point after its defer statement, before mu.Lock.
func f(done chan bool) {
	defer func() {
		n++
		mu.Unlock()
	}()
	step()
	mu.Lock()
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
