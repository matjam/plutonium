package main

import "pluto/sync"

var (
	o   sync.Once
	cfg int
	xs  = [3]int{1, 2, 3}
)

func load() {
	cfg = 5
}

func Each(f func(x int) bool) {
	for _, x := range xs {
		if !f(x) {
			return
		}
	}
}

func use(n int) {
	println(n)
}

func worker(done chan bool) {
	o.Do(load)
	Each(func(x int) bool { use(cfg + x); return true })
	done <- true
}

func main() {
	done := make(chan bool)
	task worker(done)
	task worker(done)
	<-done
	<-done
}
