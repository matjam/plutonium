package main

import "pluto/arena"

func work(done chan int) {
	a := arena.NewFixed(1024)
	p := a.New[int]()
	*p = 1
	n := *p
	a.Reset()
	done <- n
}

func main() {
	done := make(chan int)
	task work(done)
	task work(done)
	println(<-done + <-done)
}
