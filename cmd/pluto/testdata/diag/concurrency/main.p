package main

type Job struct {
	id   int
	data shared[int]
}

type Counter struct{ n int }

func (c *Counter) Run() { c.n++ }

func work(p *int) {}

func sum(xs []int) {}

func main() {
	jobs := make(chan Job, 4)
	_ = jobs

	x := 1
	task work(&x)

	var buf [8]int
	task sum(buf[:])

	var c Counter
	task c.Run()

	ptrs := make(chan *int, 1)
	ptrs <- &x
}
