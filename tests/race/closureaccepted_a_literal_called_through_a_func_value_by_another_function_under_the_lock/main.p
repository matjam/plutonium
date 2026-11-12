package main

import "pluto/sync"

// A function literal called through a func value by a function of another
// unit counts as called where its creator calls into that function, under
// the locks held there: every write of c.v holds c.mu (language spec
// section 19.3, Rule 1).

type Cache struct {
	mu sync.Mutex
	v  int
}

var cache Cache

type holder struct {
	f func()
}

func call(h *holder) { h.f() }

func get(c *Cache) {
	h := holder{f: func() { c.v++ }}
	c.mu.Lock()
	call(&h)
	c.mu.Unlock()
}

func worker(done chan int) {
	get(&cache)
	done <- 1
}

func main() {
	done := make(chan int)
	task worker(done)
	task worker(done)
	println(<-done + <-done)
}
