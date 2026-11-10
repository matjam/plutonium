package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func release() {
	mu.Unlock()
}

func main() {
	var s sync.Scope
	mu.Lock()
	n++
	s.Task(func() { release() })
	s.Wait()
}
