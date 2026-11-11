package main

import (
	"pluto/sync"
	"pluto/vec"
)

var (
	mu    sync.Mutex
	items vec.Vec[int]
)

func grow(done chan bool) {
	mu.Lock()
	items.Push(1)
	mu.Unlock()
	done <- true
}

func peek(fast bool) {
	if fast {
		mu.Lock()
	}
	p := items.Ptr(0)
	println(*p)
	if fast {
		mu.Unlock()
	}
}

func main() {
	done := make(chan bool)
	items.Push(0)
	task grow(done)
	peek(true)
	<-done
}
