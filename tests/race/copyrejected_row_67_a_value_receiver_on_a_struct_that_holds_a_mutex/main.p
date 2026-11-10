package main

import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

// Get has a value receiver, so a call copies the Counter and its mutex.
func (c Counter) Get() int { return c.n }

var total Counter

func main() {
	p := &total
	println(total.Get(), p.Get())
}
