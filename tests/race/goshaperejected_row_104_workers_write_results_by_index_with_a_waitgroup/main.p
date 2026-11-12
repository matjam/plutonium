package main

import "pluto/sync"

var (
	wg      sync.WaitGroup
	store   [4]int
	results = store[:]
)

func worker(i int) {
	defer wg.Done()
	results[i] = i * i
}

func main() {
	for i := range 4 {
		wg.Add(1)
		task worker(i)
	}
	wg.Wait()
	println(results[3])
}
