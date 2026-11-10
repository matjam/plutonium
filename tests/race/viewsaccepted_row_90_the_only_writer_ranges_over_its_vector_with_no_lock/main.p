package main

import (
	"pluto/sync"
	"pluto/vec"
)

var (
	mu    sync.Mutex
	items vec.Vec[int]
)

// writer is the only writer of items, and runs once.
func writer(done chan bool) {
	for i := 0; i < 4; i++ {
		mu.Lock()
		items.Push(i)
		mu.Unlock()
	}
	sum := 0
	for _, x := range items.Slice() { // the only writer needs no lock
		sum += x
	}
	println(sum)
	done <- true
}

func reader(done chan bool) {
	mu.Lock()
	println(items.Len())
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool, 3)
	task writer(done)
	task reader(done)
	task reader(done)
	<-done
	<-done
	<-done
}
