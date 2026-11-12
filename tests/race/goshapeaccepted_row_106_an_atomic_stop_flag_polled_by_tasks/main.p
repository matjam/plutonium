package main

import (
	"pluto/rt"
	"pluto/sync/atomic"
)

var stop atomic.Bool

func poller(done chan int) {
	n := 0
	for !stop.Load() {
		n++
		rt.Yield()
	}
	done <- n
}

func stopper() {
	stop.Store(true)
}

func main() {
	done := make(chan int)
	task poller(done)
	task poller(done)
	task stopper()
	<-done
	<-done
	println("stopped")
}
