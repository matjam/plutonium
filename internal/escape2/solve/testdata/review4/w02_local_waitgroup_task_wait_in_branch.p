package main

import (
	"pluto/sync"
	"pluto/time"
)

func work() { time.Sleep(30 * time.Millisecond) }

//pu:noinline
func f(wait bool) {
	var wg sync.WaitGroup
	wg.Task(work)
	if wait {
		wg.Wait()
	}
}

//pu:noinline
func victim() int {
	var a [64]int64
	for i := range a {
		a[i] = 1000
	}
	time.Sleep(80 * time.Millisecond)
	bad := 0
	for i := range a {
		if a[i] != 1000 {
			println("a[", i, "] changed to", a[i])
			bad++
		}
	}
	return bad
}

func main() {
	f(false)
	println("changed words:", victim())
}
