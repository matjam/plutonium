package main

import (
	"pluto/sync"
	"pluto/vec"
)

var (
	mu    sync.Mutex
	items vec.Vec[int]
)

func use(p *int, done chan bool) {
	println(*p)
	done <- true
}

func grow(done chan bool) {
	mu.Lock()
	items.Push(1)
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	items.Push(0)
	task grow(done)
	mu.Lock()
	p := items.Ptr(0)
	task use(p, done)
	mu.Unlock()
	<-done
	<-done
}
