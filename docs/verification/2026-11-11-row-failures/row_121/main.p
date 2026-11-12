package main

import "pluto/sync"

var (
	mu    sync.Mutex
	count int
)

// Each reads count, which mu guards, before its first yield.
func Each(yield func(int) bool) {
	n := count
	for i := 0; i < n; i++ {
		if !yield(i - 1) {
			return
		}
	}
}

func setter(done chan bool) {
	mu.Lock()
	count = 3
	mu.Unlock()
	done <- true
}

func scan() {
	mu.Lock()
	for x := range Each {
		if x < 0 {
			mu.Unlock()
			break
		}
	}
}

func main() {
	done := make(chan bool)
	task setter(done)
	scan()
	<-done
}
