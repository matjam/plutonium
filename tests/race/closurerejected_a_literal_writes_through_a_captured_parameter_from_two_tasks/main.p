package main

// A function literal that writes through a parameter it captures writes
// what the caller passed: two tasks write one Cache with no lock (language
// spec section 19.3, Rule 1).

type Cache struct {
	v int
}

var cache Cache

func get(c *Cache) {
	func() { c.v++ }()
}

func worker(done chan int) {
	get(&cache)
	done <- 1
}

func main() {
	done := make(chan int)
	task worker(done)
	task worker(done)
	println(<-done + <-done)
}
