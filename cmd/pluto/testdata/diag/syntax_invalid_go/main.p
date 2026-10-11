package main

func worker(jobs chan int) {
	for range jobs {
	}
}

func main() {
	jobs := make(chan int)
	go worker(jobs)
	go func() {
		jobs <- 1
	}()
}
