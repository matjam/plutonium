package main

import "pluto/sync"

// A field written only inside a Do of a Once field of the same object, and
// read only after that Do returned, needs no lock (language spec section
// 19.3, Lazy initialization), also through a parameter of an exported
// function.

type Cache struct {
	once sync.Once
	v    int
}

//pu:export
func get(c *Cache) int {
	c.once.Do(func() { c.v = 42 })
	return c.v
}

func worker(done chan int) { done <- 1 }

func main() {
	done := make(chan int)
	task worker(done)
	println(<-done)
}
