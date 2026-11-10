package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func main() {
	var s sync.Scope
	mu.Lock()
	n++
	s.Task(func() { mu.Unlock() })
	s.Wait()
}
