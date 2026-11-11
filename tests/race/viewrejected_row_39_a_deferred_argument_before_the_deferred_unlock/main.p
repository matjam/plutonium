package main

import (
	"pluto/sync"
	"pluto/vec"
)

var (
	mu    sync.Mutex
	items vec.Vec[int]
)

func use(p *int) { println(*p) }

func grow(done chan bool) {
	mu.Lock()
	items.Push(1)
	mu.Unlock()
	done <- true
}

func peek() {
	mu.Lock()
	defer use(items.Ptr(0))
	defer mu.Unlock()
}

func main() {
	done := make(chan bool)
	items.Push(0)
	task grow(done)
	peek()
	<-done
}
