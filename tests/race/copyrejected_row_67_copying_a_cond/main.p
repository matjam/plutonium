package main

import "pluto/sync"

func main() {
	var mu sync.Mutex
	var c sync.Cond
	c2 := c // copies the Cond
	mu.Lock()
	c2.Signal()
	mu.Unlock()
}
