package main

import "pluto/alloc"

type Node struct{ n int }

var r shared[Node]

func reader(done chan int) {
	s := r
	done <- s.n
}

func main() {
	r = alloc.Shared[Node]()
	done := make(chan int)
	task reader(done)
	task reader(done)
	println(<-done + <-done)
}
