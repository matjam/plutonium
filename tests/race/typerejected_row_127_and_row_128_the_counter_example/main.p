package main

import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add()      { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *Counter) Peek() int { return c.n } // error: E3503, n is changed under c.mu in Add and read here without it

var counter Counter

func work(c *Counter, done chan bool) {
	c.Add()
	done <- true
}

func run(done chan bool) {
	task work(&counter, done) // error: E3501, work calls Add under c.mu, and run calls Peek, which reads n without it
	println(counter.Peek())
}

func main() {
	done := make(chan bool)
	run(done)
	<-done
}
