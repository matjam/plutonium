package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Conn struct{ id int }

var (
	mu   sync.Mutex
	free vec.Vec[owned[Conn]]
	last *Conn
)

func Get() owned[Conn] {
	mu.Lock()
	c, ok := free.Pop()
	mu.Unlock()
	if !ok {
		c = new(Conn{id: 1})
	}
	return c
}

func Put(c owned[Conn]) {
	mu.Lock()
	last = &*c
	free.Push(c)
	mu.Unlock()
}

func peek() {
	mu.Lock()
	if last != nil {
		println(last.id)
	}
	mu.Unlock()
}

func work(done chan bool) {
	c := Get()
	c.id++
	Put(c)
	peek()
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
