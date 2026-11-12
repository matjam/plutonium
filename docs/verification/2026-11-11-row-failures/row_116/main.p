package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Scores owner struct {
	mu sync.Mutex
	v  vec.Vec[int]
}

func (s *Scores) All() func(yield func(int, *int) bool) {
	return func(yield func(int, *int) bool) {
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
}

var sc = &Scores{}

func bump(done chan bool) {
	it := sc.All()
	for _, p := range it {
		*p += 1
	}
	done <- true
}

func main() {
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
