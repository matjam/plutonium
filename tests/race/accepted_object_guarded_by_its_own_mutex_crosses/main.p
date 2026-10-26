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

func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func work(c *Counter, done chan bool) {
	c.Add()
	done <- true
}

var c Counter

func main() {
	done := make(chan bool)
	task work(&c, done)
	task work(&c, done)
	<-done
	<-done
	println(c.Get())
}