package main

import "pluto/sync"

var (
	once sync.Once
	cfg  int
)

func load() {
	cfg = 42
}

//pu:export
func get() int {
	return cfg
}

func worker(done chan bool) {
	once.Do(load)
	done <- true
}

func main() {
	done := make(chan bool)
	task worker(done)
	once.Do(load)
	println(get())
	<-done
}
