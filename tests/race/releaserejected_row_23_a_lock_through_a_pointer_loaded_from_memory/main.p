package main

import "pluto/sync"

type T struct {
	mu sync.Mutex
	n  int
}

type G struct {
	p *T
}

var (
	t T
	g G
)

func work(done chan bool) {
	g.p.mu.Lock()
	g.p.n++
	g.p.mu.Unlock()
	done <- true
}

func main() {
	g.p = &t
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
