package main

import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

func worker(c *Counter) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func main() {
	c := &Counter{}
	task worker(c)
}
