package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add()      { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *Counter) Peek() int { return c.n }

// get is the pool: it hands out a *Counter from alloc.New with no owner.
func get() *Counter { return alloc.New[Counter]() }

func work(c *Counter, done chan bool) {
	c.Add()
	done <- true
}

func run(done chan bool) {
	p := get()
	task work(p, done) // E3501: work calls Add under c.mu, and run calls Peek, which reads n without it
	println(p.Peek())
}

func main() {
	done := make(chan bool)
	run(done)
	<-done
}
