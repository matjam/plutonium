package main
import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) Add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Counter) Peek() int { return c.n }

func work(c *Counter, done chan bool) {
	c.Add()
	done <- true
}

var c Counter

func main() {
	done := make(chan bool)
	task work(&c, done)
	<-done
	println(c.Peek())
}