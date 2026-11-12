package main

// A function literal assigns its captured variable c the address of
// another local Cache, and the function that declares c then writes
// through it: each call writes its own Cache, so the two tasks share
// nothing (language spec section 19.3,
// Rule 1).

type Cache struct {
	v int
}

var cache Cache

func work(p *Cache, n int) {
	var mine Cache
	c := &Cache{}
	f := func(d int) { c = &mine; c.v = d }
	f(n)
	c.v += n
}

func worker(done chan int) {
	work(&cache, 1)
	done <- 1
}

func main() {
	done := make(chan int)
	task worker(done)
	task worker(done)
	println(<-done + <-done)
}
