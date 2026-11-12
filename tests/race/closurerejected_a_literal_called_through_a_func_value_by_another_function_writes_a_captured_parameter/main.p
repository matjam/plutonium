package main

// A function literal called through a func value by a function of another
// unit writes what its captured parameter points to. That call cannot see
// the captures, so the literal counts as called where its creator calls
// into the other function: two tasks write one Cache with no lock
// (language spec section 19.3, Rule 1).

type Cache struct {
	v int
}

var cache Cache

type holder struct {
	f func()
}

func call(h *holder) { h.f() }

func get(c *Cache) {
	h := holder{f: func() { c.v++ }}
	call(&h)
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
