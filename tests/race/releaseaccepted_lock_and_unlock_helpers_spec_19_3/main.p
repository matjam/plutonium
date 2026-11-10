package main

import "pluto/sync"

type Stats struct {
	mu    sync.Mutex
	count int
}

func (s *Stats) lock()   { s.mu.Lock() }
func (s *Stats) unlock() { s.mu.Unlock() }

func (s *Stats) Add(n int) {
	s.lock()
	defer s.unlock()
	s.count += n
}

func work(s *Stats, done chan bool) {
	s.Add(1)
	done <- true
}

var the Stats

func main() {
	s := &the
	done := make(chan bool)
	task work(s, done)
	s.Add(2)
	<-done
}
