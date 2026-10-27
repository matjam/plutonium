package main

type Counter struct{ n int }

func (c *Counter) bump(done chan int) {
	c.n++
	done <- c.n
}

func main() {
	done := make(chan int)
	c := new(Counter)
	p := &c.n
	*p = 5
	task c.bump(done)
	println(<-done)
}
