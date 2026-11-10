package main

import "pluto/sync"

var (
	mu    sync.Mutex
	ready bool
)

func producer() {
	mu.Lock()
	ready = true
	task finish() // the new task starts with no lock held
}

func finish() {
	mu.Unlock() // finish does not hold mu
}

func main() {
	producer()
}
