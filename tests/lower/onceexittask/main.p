package main

// A function passed to Once.Do that ends its task with rt.ExitTask leaves
// the Once unlocked and not done (language spec section 19.3, Lazy
// initialization, When f does not return): the deferred calls of the task
// run, the task ends, and the next Do calls its own function. A Do whose
// function returns is the one that makes the Once done.

import (
	"pluto/rt"
	"pluto/sync"
)

var (
	once sync.Once
	mu   sync.Mutex
	n    int
)

func quit() {
	mu.Lock()
	n = 1
	mu.Unlock()
	rt.ExitTask()
	println("not reached: quit")
}

func load() {
	mu.Lock()
	n = n + 10
	mu.Unlock()
}

func worker(done chan bool) {
	defer func() { done <- true }()
	once.Do(quit)
	println("not reached: worker")
}

func main() {
	done := make(chan bool)
	task worker(done)
	<-done
	once.Do(load)
	once.Do(quit)
	mu.Lock()
	println(n)
	mu.Unlock()
}
