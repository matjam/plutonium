package main

import "pluto/rt"

var events = make(chan int, 16)

func record(n int) {
	if n > 0 {
		events <- n // blocks while the buffer is full
	}
}

func tick(n int) {
	rt.DisablePreemption()
	record(n) // error: a direct call reaches a send that may block, inside the non-migrating region
	rt.EnablePreemption()
}

func main() {
	tick(1)
	println(<-events)
}
