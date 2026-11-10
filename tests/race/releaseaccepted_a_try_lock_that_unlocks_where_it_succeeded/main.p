package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func attempt() {
	if mu.TryLock() {
		n++
		mu.Unlock()
	}
	ok := mu.TryLock()
	if !ok {
		return
	}
	n++
	mu.Unlock()
}

func main() {
	attempt()
	attempt()
	println(n)
}
