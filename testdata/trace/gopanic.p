package main

// A panic in a puroutine names the puroutine and prints its stack, which
// ends at the function the go statement started.

func level2(xs []int, i int) int {
	return xs[i] * 2
}

func worker(xs []int, i int, done chan int) {
	done <- level2(xs, i+2)
}

var data = [3]int{1, 2, 3}

func main() {
	done := make(chan int)
	task worker(data[:], 0, done)
	println(<-done)
	task worker(data[:], 1, done)
	println(<-done)
}
