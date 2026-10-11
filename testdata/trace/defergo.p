package main

// A panic runs the deferred calls of the puroutine that panicked only: the
// deferred call of main, which waits for the worker, does not run.

func check(xs []int, i int) int {
	defer println("check deferred, i =", i)
	return xs[i]
}

func worker(xs []int, done chan int) {
	defer println("worker deferred")
	n := check(xs, 0)
	done <- n + check(xs, 3)
}

var data = [3]int{1, 2, 3}

func main() {
	defer println("main deferred: not run")
	done := make(chan int)
	task worker(data[:], done)
	println(<-done)
}
