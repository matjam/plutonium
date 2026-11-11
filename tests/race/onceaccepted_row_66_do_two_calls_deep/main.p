package main

import "pluto/sync"

var (
	once sync.Once
	data int
)

func f() {
	data = 42
}

func load() {
	once.Do(f)
}

func cfg() int {
	load()
	return data
}

func worker(done chan bool) {
	println(cfg())
	done <- true
}

func main() {
	done := make(chan bool)
	task worker(done)
	task worker(done)
	<-done
	<-done
}
