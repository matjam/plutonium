package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Scores owner struct {
	mu   sync.Mutex
	v    vec.Vec[int]
	next *Scores
}

func (s *Scores) All() func(yield func(int, *int) bool) {
	f := func(yield func(int, *int) bool) {
		for i := 0; ; i++ {
			s.mu.Lock()
			if i >= s.v.Len() {
				s.mu.Unlock()
				return
			}
			ok := yield(i, s.v.Ptr(i))
			s.mu.Unlock()
			if !ok {
				return
			}
		}
	}
	s = s.next
	return f
}

var sc = &Scores{}

func bump(done chan bool) {
	for _, p := range sc.All() {
		*p += 1
	}
	done <- true
}

func main() {
	sc.next = sc
	sc.v.Push(1)
	sc.v.Push(2)
	done := make(chan bool)
	task bump(done)
	task bump(done)
	<-done
	<-done
	sc.mu.Lock()
	println(sc.v.At(0), sc.v.At(1))
	sc.mu.Unlock()
}
