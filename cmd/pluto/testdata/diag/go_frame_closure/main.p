package main

type counter struct{ n int }

func (c *counter) add() { c.n++ }

func run(f func(), done chan bool) {
	f()
	done <- true
}

func main() {
	n := 3
	out := make(chan int, 1)
	done := make(chan bool, 3)
	square := func() { out <- n * n }
	go square()

	var c counter
	go run(c.add, done)

	inc := c.add
	again := inc
	go run(again, done)

	go run(func() { n++ }, done)

	// Not reported: assigned twice, so only the go statement can tell.
	maybe := func() {}
	maybe = func() { n-- }
	go run(maybe, done)
	println(<-out, <-done)
}
