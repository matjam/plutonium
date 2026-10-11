package main

// A panic runs the panicking puroutine's deferred calls while the other
// puroutines go on: the deferred Unlock lets the waiting worker take the
// lock, and the deferred receive waits for it to finish before the panic
// is reported.

import "pluto/sync"

var mu sync.Mutex

func worker(done chan bool) {
	mu.Lock()
	println("worker took the lock")
	mu.Unlock()
	done <- true
}

func update(xs []int, i int) {
	mu.Lock()
	defer mu.Unlock()
	xs[i]++
}

func main() {
	done := make(chan bool)
	defer func() {
		<-done
		println("worker finished")
	}()
	xs := []int{0, 0}
	update(xs, 1)
	mu.Lock()
	task worker(done)
	mu.Unlock()
	update(xs, 2)
}
