package main

import "pluto/sync"

type S struct {
	mu sync.Mutex
	n  int
}

func (s *S) withLock(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func work(s *S, done chan bool) {
	s.withLock(func() { s.n++ })
	done <- true
}

var the S

func main() {
	s := &the
	done := make(chan bool)
	task work(s, done)
	task work(s, done)
	<-done
	<-done
}
