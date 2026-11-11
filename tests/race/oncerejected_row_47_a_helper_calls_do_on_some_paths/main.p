package main

import "pluto/sync"

var (
	once sync.Once
	cfg  int
)

func load() {
	cfg = 42
}

func maybe(fast bool) {
	if fast {
		return
	}
	once.Do(load)
}

func reader(fast bool, done chan bool) {
	maybe(fast)
	println(cfg)
	done <- true
}

func main() {
	done := make(chan bool)
	task reader(true, done)
	task reader(false, done)
	<-done
	<-done
}
