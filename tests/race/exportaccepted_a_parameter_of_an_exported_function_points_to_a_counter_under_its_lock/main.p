package main

import "pluto/sync"

// The twin of exportrejected_a_parameter_of_an_exported_function_points_to_a_counter_it_changes:
// the Counter's field is used only under a lock of the same Counter.

type Counter struct {
	mu sync.Mutex
	n  int
}

//pu:export
func bump(c *Counter) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func worker(done chan int) { done <- 1 }

func main() {
	done := make(chan int)
	task worker(done)
	println(<-done)
}
