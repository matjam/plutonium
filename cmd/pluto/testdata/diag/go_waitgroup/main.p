package main

import (
	stdsync "pluto/sync"
	"pluto/sync/atomic"
)

type stats struct {
	mu stdsync.Mutex
	n  int
}

func main() {
	var wg stdsync.WaitGroup
	results := make([]int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = i * i
		}()
	}
	wg.Wait()

	var s stats
	var hits atomic.Int64
	go func() {
		s.mu.Lock()
		s.n += int(hits.Load())
		s.mu.Unlock()
	}()

	n := 1
	go func() { println(n) }()
}
