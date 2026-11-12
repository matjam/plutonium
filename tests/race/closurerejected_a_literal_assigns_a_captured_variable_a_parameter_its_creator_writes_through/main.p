package main

// A function literal assigns its captured variable c the parameter p, and
// the function that declares c then writes through it: two tasks write one
// Cache with no lock (language spec section 19.3, Rule 1).

type Cache struct {
	v int
}

var cache Cache

func work(p *Cache, n int) {
	c := &Cache{}
	f := func(d int) { c = p }
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
