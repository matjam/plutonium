package main

// A freestanding program that starts a puroutine without binding a
// scheduler.

func worker(done chan bool) { done <- true }

func main() {
	done := make(chan bool)
	task worker(done)
	<-done
}
