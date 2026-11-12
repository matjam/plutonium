package main

import "pluto/sync"

var wg sync.WaitGroup

func work(n int) { println(n) }

func main() {
	items := []int{1, 2, 3}
	for i := range items {
		wg.Add(1)
		task func() {
			defer wg.Done()
			work(items[i])
		}()
	}
	wg.Wait()
}
