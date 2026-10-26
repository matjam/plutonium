package main

import (
	stdsync "pluto/sync"
	"pluto/sync/atomic"
)

type stats struct {
	mu stdsync.Mutex
	n  int
}

var results [4]int

func main() {
	var wg stdsync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		task func() {
			defer wg.Done()
			results[i] = i * i
		}()
	}
	wg.Wait()

	var s stats
	var hits atomic.Int64
	task func() {
		s.mu.Lock()
		s.n += int(hits.Load())
		s.mu.Unlock()
	}()

	n := 1
	task func() { println(n) }()
}
