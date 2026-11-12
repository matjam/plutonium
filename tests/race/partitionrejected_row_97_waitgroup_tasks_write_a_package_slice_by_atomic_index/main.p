package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
)

var (
	wg      sync.WaitGroup
	next    atomic.Int64
	store   [4]int
	results = store[:]
)

func work() {
	results[next.Add(1)-1] = 1
}

func main() {
	for i := 0; i < 4; i++ {
		wg.Task(work)
	}
	wg.Wait()
	println(results[0])
}
