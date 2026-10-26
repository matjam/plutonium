package main

import (
	"pluto/sync"
	"pluto/time"
)

func work() { time.Sleep(30 * time.Millisecond) }

func start(wg *sync.WaitGroup) { wg.Task(work) }

//pu:noinline
func f() {
	var wg sync.WaitGroup
	start(&wg)
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
	f()
	println("changed words:", victim())
}
