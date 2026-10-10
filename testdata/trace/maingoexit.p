package main

// rt.Goexit in the puroutine that runs main ends it after its deferred
// calls, without main returning: the other puroutines go on, and once none
// is left the program stops with status 2, as Go's does.

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
	defer println("main deferred")
	defer func() { done <- true }()
	go worker(done)
	finish()
	println("not reached")
}
