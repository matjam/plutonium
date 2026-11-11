package main

import "pluto/sync"

var (
	once sync.Once
	cfg  int
)

func load() {
	cfg = 42
}

func reader(done chan bool) {
	println(cfg)
	done <- true
}

func main() {
	done := make(chan bool)
	task reader(done)
	once.Do(load)
	println(cfg)
	<-done
}
