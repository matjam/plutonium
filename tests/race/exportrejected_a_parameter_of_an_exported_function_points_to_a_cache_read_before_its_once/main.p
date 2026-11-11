package main

import "pluto/sync"

// The twin of exportaccepted_a_parameter_of_an_exported_function_points_to_a_cache_set_in_its_once:
// the read comes before the Do, so another call may be writing the field.

type Cache struct {
	once sync.Once
	v    int
}

//pu:export
func get(c *Cache) int {
	v := c.v
	c.once.Do(func() { c.v = 42 })
	return v
}

func worker(done chan int) { done <- 1 }

func main() {
	done := make(chan int)
	task worker(done)
	println(<-done)
}
