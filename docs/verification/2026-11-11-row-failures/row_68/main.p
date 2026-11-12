package main

import "pluto/sync"

type Box owner struct {
	mu sync.Mutex
	n  int
}

func worker(ch chan Box, done chan int) {
	b := <-ch // the receiver starts with nothing held
	done <- b.n
}

func main() {
	var b Box
	b.mu.Lock()
	b.n = 2
	ch := make(chan Box, 1)
	ch <- b // the move ends the name b, and with it the held lock
	done := make(chan int)
	task worker(ch, done)
	println(<-done)
}
