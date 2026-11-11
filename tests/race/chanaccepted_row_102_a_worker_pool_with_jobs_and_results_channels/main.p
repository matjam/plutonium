package main

import "pluto/sync"

var wg sync.WaitGroup

func worker(jobs chan int, results chan int) {
	for j := range jobs {
		results <- j * 2
	}
	wg.Done()
}

func closer(results chan int) {
	wg.Wait()
	close(results)
}

func main() {
	jobs := make(chan int, 10)
	results := make(chan int, 10)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		task worker(jobs, results)
	}
	for i := 0; i < 5; i++ {
		jobs <- i
	}
	close(jobs)
	task closer(results)
	sum := 0
	for r := range results {
		sum += r
	}
	println(sum)
}
