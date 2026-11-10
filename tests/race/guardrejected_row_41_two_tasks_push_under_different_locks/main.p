package main

import (
	"pluto/sync"
	"pluto/vec"
)

var (
	a, b  sync.Mutex
	items vec.Vec[int]
)

func pushA(done chan bool) {
	a.Lock()
	items.Push(1)
	a.Unlock()
	done <- true
}

func pushB(done chan bool) {
	b.Lock()
	items.Push(2)
	b.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task pushA(done)
	task pushB(done)
	<-done
	<-done
	println(items.Len())
}
