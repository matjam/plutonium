package main

import (
	"pluto/sync"
	"pluto/vec"
)

// second keeps a pointer into names across a Wait.
var (
	mu    sync.Mutex
	ready sync.Cond
	names vec.Vec[string]
)

func add(s string) {
	mu.Lock()
	names.Push(s)
	mu.Unlock()
	ready.Broadcast()
}

func second() {
	mu.Lock()
	for names.Len() < 1 {
		ready.Wait(&mu)
	}
	p := names.Ptr(0)
	for names.Len() < 2 {
		ready.Wait(&mu)
	}
	println(*p) // error: E3501, p was loaded under mu and is used after a Wait released mu
	mu.Unlock()
}

func start() {
	task second()
	add("a")
	add("b")
}

func main() {
	start()
}
