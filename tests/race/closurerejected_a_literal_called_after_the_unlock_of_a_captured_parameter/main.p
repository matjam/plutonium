package main

import (
	"pluto/alloc"
	"pluto/sync"
)

// A function literal called in place is part of the enclosing function: here
// it writes through a captured parameter after the caller unlocks, so two
// tasks may write one Cache at once (language spec section 19.3, Rule 3; Part
// of the enclosing function).

type Cache struct {
	mu sync.Mutex
	v  int
}

func set(c *Cache) {
	c.mu.Lock()
	c.mu.Unlock()
	func() { c.v = 42 }()
}

func worker(cs chan *Cache, done chan int) {
	set(<-cs)
	done <- 1
}

func main() {
	done := make(chan int)
	cs := make(chan *Cache, 2)
	task worker(cs, done)
	task worker(cs, done)
	c := alloc.New[Cache]()
	cs <- c
	cs <- c
	println(<-done + <-done)
}
