package main

import (
	"pluto/alloc"
	"pluto/sync"
)

// A field written only in a method that a Once field of the same object runs
// through Do, and read only after that Do returned, needs no lock (language
// spec section 19.3, Lazy initialization): the method value's receiver is
// the object.

type Cache struct {
	once sync.Once
	v    int
}

func (c *Cache) fill() { c.v = 42 }

func (c *Cache) get() int {
	c.once.Do(c.fill)
	return c.v
}

func worker(cs chan *Cache, done chan int) {
	c := <-cs
	done <- c.get()
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
