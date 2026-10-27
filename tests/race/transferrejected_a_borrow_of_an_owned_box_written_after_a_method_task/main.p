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
	task c.bump(done)
	*p = 5
	println(<-done)
}
