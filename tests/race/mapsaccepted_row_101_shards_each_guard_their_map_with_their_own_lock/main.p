package main

import (
	"pluto/alloc"
	"pluto/sync"
)

// Row 101 of section 19.3: the Go shape, a struct with a mutex and a map,
// several instances, each method locking its own mutex. The map stays in
// its field (Maps kept in place), so each shard's lock guards it.

type Shard struct {
	mu sync.Mutex
	m  map[int]int
}

func (s *Shard) Add(k int) {
	s.mu.Lock()
	s.m[k]++
	s.mu.Unlock()
}

func (s *Shard) Count(k int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k]
}

var shards [4]*Shard

func work(id int, done chan bool) {
	for i := 0; i < 100; i++ {
		shards[(id+i)%4].Add(i)
	}
	done <- true
}

func main() {
	for i := range shards {
		s := alloc.New[Shard]()
		s.m = make(map[int]int)
		shards[i] = s
	}
	done := make(chan bool)
	for id := 0; id < 4; id++ {
		task work(id, done)
	}
	for id := 0; id < 4; id++ {
		<-done
	}
	println(shards[0].Count(1))
}
