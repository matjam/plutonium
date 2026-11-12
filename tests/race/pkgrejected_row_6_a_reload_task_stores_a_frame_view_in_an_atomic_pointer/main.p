package main

import "pluto/sync/atomic"

type Config struct{ port int }

var cfg atomic.Pointer[Config]

func reload(done chan bool) {
	cfg.Store(&Config{port: 80})
	done <- true
}

func main() {
	done := make(chan bool)
	task reload(done)
	<-done
}
