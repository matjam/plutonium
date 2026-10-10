package main

// rt.Goexit in the puroutine that runs main ends it after its deferred
// calls, without main returning: the other puroutines go on, and once none
// is left the program stops with status 2, as Go's does.
//
// The worker is released by main's last deferred call, after everything main
// prints: the worker runs on another thread, and two puroutines that print
// at the same time interleave their output.

import "pluto/rt"

func worker(done chan bool) {
	<-done
	println("worker runs after main's Goexit")
}

func finish() {
	defer println("finish deferred")
	rt.Goexit()
}

func main() {
	done := make(chan bool, 1)
	defer func() { done <- true }()
	defer println("main deferred")
	go worker(done)
	finish()
	println("not reached")
}
